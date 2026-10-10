package wire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/wake"
)

type Container struct {
	memory             *memory.Engine
	clock              func() time.Time
	cfg                *config.Config
	store              *store.Store
	runnerFactory      *runtime.RunnerFactory
	runController      *runtime.RunController
	runResume          *api.RunResumeService
	skills             *api.SkillService
	threads            *api.ThreadService
	runs               *api.RunService
	events             *api.EventService
	pendingAction      *api.PendingActionService
	capabilities       *api.CapabilitiesService
	deviceAuth         *api.DeviceAuthService
	inbox              *api.InboxService
	vault              *knowledge.Vault
	knowledge          *api.KnowledgeService
	captures           *api.CaptureService
	phoneNotifications *api.PhoneNotificationService
	wake               *wake.Scheduler
	// watchBrowser renders web_rendered watches; nil without a browser.
	watchBrowser *tools.Service
}

// buildOptions are the process-level dependencies of a container.
type buildOptions struct {
	// clock drives presence, commitments and notifications.
	clock func() time.Time
	// fcmEndpoint, when set, replaces the FCM API send endpoint.
	fcmEndpoint string
	// githubAPI, when set, replaces the GitHub API base URL for watches.
	githubAPI string
}

func NewContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	return buildContainer(ctx, cfg, buildOptions{clock: time.Now})
}

func (c *Container) Config() *config.Config {
	return c.cfg
}

// ResumeReadyRuns resumes every interrupted run whose pending actions are
// all decided.
func (c *Container) ResumeReadyRuns(ctx context.Context) error {
	return c.runResume.ResumeReadyRuns(ctx)
}

func (c *Container) Skills() *api.SkillService {
	return c.skills
}

func (c *Container) Capabilities() *api.CapabilitiesService {
	return c.capabilities
}

func (c *Container) DeviceAuth() *api.DeviceAuthService {
	return c.deviceAuth
}

// Handler is the /v1 client API over this container's services.
func (c *Container) Handler(logger *slog.Logger) (http.Handler, error) {
	return api.NewHandler(api.Dependencies{
		Threads:            c.threads,
		Runs:               c.runs,
		Events:             c.events,
		PendingAction:      c.pendingAction,
		Skills:             c.skills,
		Capabilities:       c.capabilities,
		DeviceAuth:         c.deviceAuth,
		Inbox:              c.inbox,
		Knowledge:          c.knowledge,
		Captures:           c.captures,
		PhoneNotifications: c.phoneNotifications,
		Config:             c.cfg,
		Logger:             logger,
	})
}

// KnowledgeStatus reports the knowledge base for acorn doctor.
func (c *Container) KnowledgeStatus(ctx context.Context) (knowledge.Status, error) {
	return c.vault.Status(ctx)
}

// Watches lists the watches for acorn doctor.
func (c *Container) Watches(ctx context.Context) ([]core.Watch, error) {
	return c.store.ListWatches(ctx)
}

// WakeScheduler keeps commitments; serve runs it.
func (c *Container) WakeScheduler() *wake.Scheduler {
	return c.wake
}

// wakeInterval is how often the wake scheduler looks for due commitments.
const wakeInterval = 30 * time.Second

func (c *Container) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.runnerFactory != nil {
		if err := c.runnerFactory.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.watchBrowser != nil {
		if err := c.watchBrowser.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.store != nil {
		if err := c.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func buildContainer(ctx context.Context, cfg *config.Config, options buildOptions) (*Container, error) {
	runtime.RegisterTypes()

	store, err := store.Open(cfg.Runtime.StorageDir)
	if err != nil {
		return nil, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = store.Close()
		}
	}()

	deps, err := buildContainerRuntimeDeps(ctx, cfg, store, options)
	if err != nil {
		return nil, err
	}

	container, err := buildContainerAppServices(cfg, store, deps, options.clock)
	if err != nil {
		return nil, err
	}
	container.store = store

	committed = true
	return container, nil
}

func buildContainerAppServices(cfg *config.Config, db *store.Store, deps *containerRuntimeDeps, clock func() time.Time) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		memory:        deps.memory,
		clock:         clock,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.runResume = api.NewRunResumeService(db).WithResume(deps.resumeRun)
	container.skills = api.NewSkillService(cfg, deps.loader)
	container.threads = api.NewThreadService(db)
	container.runs = api.NewRunService(db, container.threads, deps.executeRun, deps.runController).WithResumer(container.runResume)
	container.events = api.NewEventService(db, db)
	container.pendingAction = api.NewPendingActionService(db).WithResumer(container.runResume)

	container.capabilities = api.NewCapabilitiesService(cfg, container.skills.Snapshot, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = api.NewDeviceAuthService(db).WithPushTokens(db)
	container.inbox = api.NewInboxService(db, container.capabilities)
	container.vault = deps.vault
	container.knowledge = api.NewKnowledgeService(deps.vault)
	container.captures = api.NewCaptureService(deps.vault, container.threads, container.runs)

	phoneNotifications, err := api.NewPhoneNotificationService(db, clock)
	if err != nil {
		return nil, err
	}
	container.phoneNotifications = phoneNotifications
	location, err := cfg.OwnerLocation()
	if err != nil {
		return nil, err
	}
	briefing, err := briefingSchedule(cfg)
	if err != nil {
		return nil, err
	}
	thinking, err := thinkingSchedule(cfg)
	if err != nil {
		return nil, err
	}
	container.watchBrowser = deps.watchBrowser
	container.wake, err = wake.NewScheduler(wake.Config{
		Routines:           db,
		PhoneNotifications: db,
		Store:              db,
		Commitments:        db,
		Memory:             db,
		Events:             db,
		Runs:               &wakeRunStarter{runs: container.runs, store: db},
		Watches:            db,
		Checker:            deps.watchChecker,
		Clock:              clock,
		Location:           location,
		DailyLimit:         cfg.Wake.DailyLimit,
		DailyTokens:        cfg.Wake.DailyTokens,
		Thinking:           thinking,
		MaxChecksPerTick:   cfg.Watch.MaxChecksPerTick,
		Briefing:           briefing,
		Interval:           wakeInterval,
	})
	if err == nil && deps.notifier != nil {
		container.wake = container.wake.WithNotifications(deps.notifier)
	}
	if err != nil {
		return nil, err
	}

	return container, nil
}

// wakeRunStarter starts commitment wake runs through the run service.
type wakeRunStarter struct {
	runs  *api.RunService
	store core.SessionStore
}

// remindersThreadTitle names the thread that receives wakes whose original
// conversation was deleted.
const remindersThreadTitle = "Reminders"

func (w *wakeRunStarter) StartWakeRun(ctx context.Context, threadID, input string, wake core.ScheduledWake) (string, error) {
	_, err := w.store.LoadSession(ctx, threadID)
	if errors.Is(err, core.ErrSessionNotFound) {
		threadID = core.NewSessionID()
		if _, err := w.store.CreateSession(ctx, threadID, remindersThreadTitle); err != nil {
			return "", fmt.Errorf("create reminders thread: %w", err)
		}
	} else if err != nil {
		return "", err
	}
	run, err := w.runs.CreateScheduledRun(ctx, threadID, input, wake)
	if err != nil {
		return "", err
	}
	return run.ID, nil
}

func (w *wakeRunStarter) StartRoutineRun(ctx context.Context, threadID, title, input string, wake core.ScheduledWake) (string, string, error) {
	if threadID != "" {
		if _, err := w.store.LoadSession(ctx, threadID); errors.Is(err, core.ErrSessionNotFound) {
			threadID = ""
		} else if err != nil {
			return "", "", err
		}
	}
	if threadID == "" {
		threadID = core.NewSessionID()
		if _, err := w.store.CreateSession(ctx, threadID, title); err != nil {
			return "", "", fmt.Errorf("create routine thread: %w", err)
		}
	}
	run, err := w.runs.CreateScheduledRun(ctx, threadID, input, wake)
	if err != nil {
		return "", "", err
	}
	return threadID, run.ID, nil
}

// RunOnceResult is the terminal outcome of an owner-local smoke run.
type RunOnceResult struct {
	RunID  string
	Status string
	Output string
	Error  string
}

// RunOnce executes a single owner-local run synchronously and returns its
// terminal result. It is an operator smoke probe: it drives the exact runtime
// execution path (Executor -> RunnerFactory -> ChatModelAgent),
// so any readiness gap (unconfigured embedding,
// prepare failure) surfaces here as a real error or failed result instead of
// staying hidden until the first remote-client message.
func (c *Container) RunOnce(ctx context.Context, input string) (*RunOnceResult, error) {
	if c == nil {
		return nil, errors.New("container is nil")
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, errors.New("run input is required")
	}
	exec, err := runtime.NewExecutorWithRunRuntimeAndController(c.cfg, c.store, c.runnerFactory, c.runController)
	if err != nil {
		return nil, err
	}
	result, err := exec.ExecuteMessages(ctx, core.ExecuteRequest{
		Input: trimmed,
	}, nil)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("runtime executor returned nil result")
	}
	return &RunOnceResult{
		RunID:  result.RunID,
		Status: string(result.Status),
		Output: result.Output,
		Error:  result.Error,
	}, nil
}
