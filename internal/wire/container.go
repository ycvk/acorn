package wire

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/wake"
)

type Container struct {
	cfg           *config.Config
	store         *store.Store
	runnerFactory *runtime.RunnerFactory
	runController *runtime.RunController
	runResume     *api.RunResumeService
	skills        *api.SkillService
	threads       *api.ThreadService
	runs          *api.RunService
	events        *api.EventService
	pendingAction *api.PendingActionService
	memory        memory.Service
	capabilities  *api.CapabilitiesService
	deviceAuth    *api.DeviceAuthService
	inbox         *api.InboxService
	wake          *wake.Scheduler
}

func NewContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	return buildContainer(ctx, cfg)
}

func (c *Container) Config() *config.Config {
	return c.cfg
}

// ResumeReadyRuns resumes every interrupted run whose pending actions are
// all decided.
func (c *Container) ResumeReadyRuns(ctx context.Context) error {
	return c.runResume.ResumeReadyRuns(ctx)
}

func (c *Container) Threads() *api.ThreadService {
	return c.threads
}

func (c *Container) Runs() *api.RunService {
	return c.runs
}

func (c *Container) Events() *api.EventService {
	return c.events
}

func (c *Container) PendingAction() *api.PendingActionService {
	return c.pendingAction
}

func (c *Container) Skills() *api.SkillService {
	return c.skills
}

func (c *Container) Memory() memory.Service {
	return c.memory
}

func (c *Container) Capabilities() *api.CapabilitiesService {
	return c.capabilities
}

func (c *Container) DeviceAuth() *api.DeviceAuthService {
	return c.deviceAuth
}
func (c *Container) Inbox() *api.InboxService {
	return c.inbox
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
	if c.store != nil {
		if err := c.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	// memory.Service may hold a vector index DB; close it if supported.
	if closer, ok := c.memory.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func buildContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

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

	wsDir := filepath.Join(strings.TrimSpace(cfg.Runtime.StorageDir), "worldstate")
	ws, err := memory.NewWorldState(wsDir)
	if err != nil {
		return nil, fmt.Errorf("build world state: %w", err)
	}

	deps, err := buildContainerRuntimeDeps(ctx, cfg, store, ws)
	if err != nil {
		return nil, err
	}

	container, err := buildContainerAppServices(cfg, store, deps)
	if err != nil {
		return nil, err
	}
	container.store = store

	committed = true
	return container, nil
}

func buildContextPlane(cfg *config.Config) (*runtime.ContextPlane, error) {
	contextCounter, err := runtime.NewTokenCounter()
	if err != nil {
		return nil, err
	}
	maxContextTokens := cfg.Context.WindowTokens - cfg.Context.CompactMarginTokens
	if maxContextTokens <= 0 {
		return nil, fmt.Errorf("context effective window must be positive: window=%d margin=%d", cfg.Context.WindowTokens, cfg.Context.CompactMarginTokens)
	}
	contextPlane := runtime.NewDefaultPlane(runtime.DefaultOptions{
		MemoryContextTokenBudget: cfg.Memory.Search.MemoryContextTokenBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             contextCounter,
	})
	return contextPlane, nil
}

// buildMemoryService constructs the file-backed memory service.
// When memory.embedding.enabled is true, wires EmbeddingClient + sqlite-vec.
func buildMemoryService(ctx context.Context, cfg *config.Config) (memory.Service, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	memoryRoot := strings.TrimSpace(cfg.Runtime.StorageDir)
	memCfg := memory.Config{Root: memoryRoot}
	if cfg.Memory.Embedding.Enabled {
		provider, err := cfg.EnabledProvider()
		if err != nil {
			return nil, fmt.Errorf("resolve provider for embedding: %w", err)
		}
		ec := memory.NewEmbeddingClient(memory.EmbeddingConfig{
			BaseURL:    provider.BaseURL,
			APIKey:     provider.APIKey,
			Model:      cfg.Memory.Embedding.Model,
			Dimensions: cfg.Memory.Embedding.Dimensions,
		})
		if ec == nil {
			return nil, fmt.Errorf("memory.embedding.enabled is true but provider %s has no base_url or api_key", provider.Name)
		}
		memCfg.Embedding = ec
	}
	svc, err := memory.NewLocalService(memCfg)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func buildContainerAppServices(cfg *config.Config, db *store.Store, deps *containerRuntimeDeps) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
		memory:        deps.memoryModule,
	}

	container.runResume = api.NewRunResumeService(db).WithResume(deps.resumeRun)
	container.skills = api.NewSkillService(cfg, deps.loader)
	container.threads = api.NewThreadService(db, cfg.WorkspaceRoot())
	container.runs = api.NewRunService(db, container.threads, deps.executeRun, deps.runController).WithResumer(container.runResume)
	container.events = api.NewEventService(db, db)
	container.pendingAction = api.NewPendingActionService(db).WithResumer(container.runResume)

	container.capabilities = api.NewCapabilitiesService(cfg, container.skills.Snapshot, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = api.NewDeviceAuthService(db)
	container.inbox = api.NewInboxService(db, container.capabilities)

	location, err := cfg.OwnerLocation()
	if err != nil {
		return nil, err
	}
	container.wake, err = wake.NewScheduler(wake.Config{
		Store:      db,
		Events:     db,
		Runs:       &wakeRunStarter{runs: container.runs, store: db},
		Clock:      time.Now,
		Location:   location,
		DailyLimit: cfg.Wake.DailyLimit,
		Interval:   wakeInterval,
	})
	if err != nil {
		return nil, err
	}

	return container, nil
}

// worldStateAdapter wraps memory.WorldState to satisfy tools.WorldStateUpdater.
// It translates between the tools package's WorldStateDelta and memory's.
type worldStateAdapter struct {
	ws *memory.WorldState
}

func (a *worldStateAdapter) ApplyDelta(ctx context.Context, delta tools.WorldStateDelta) error {
	return a.ws.ApplyDelta(ctx, memory.WorldStateDelta{
		Upserts: delta.Upserts,
		Deletes: delta.Deletes,
	})
}

func (a *worldStateAdapter) Load(ctx context.Context) (map[string]string, error) {
	return a.ws.Load(ctx)
}

// wakeRunStarter starts commitment wake runs through the run service.
type wakeRunStarter struct {
	runs  *api.RunService
	store core.SessionStore
}

// remindersThreadTitle names the thread that receives wakes whose original
// conversation was deleted.
const remindersThreadTitle = "Reminders"

func (w *wakeRunStarter) StartWakeRun(ctx context.Context, threadID, wake, input string) (string, error) {
	_, err := w.store.LoadSession(ctx, threadID)
	if errors.Is(err, core.ErrSessionNotFound) {
		threadID = core.NewSessionID()
		if _, err := w.store.CreateSession(ctx, threadID, remindersThreadTitle); err != nil {
			return "", fmt.Errorf("create reminders thread: %w", err)
		}
	} else if err != nil {
		return "", err
	}
	run, err := w.runs.CreateWakeRun(ctx, threadID, wake, input)
	if err != nil {
		return "", err
	}
	return run.ID, nil
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
// execution path (Executor -> RunnerFactory -> ContextPlane -> memory prepare),
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
