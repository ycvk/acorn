package wire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/triggers"
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
	triggerSched  *triggers.Scheduler
	worldState    *memory.WorldState
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

func (c *Container) RunResume() *api.RunResumeService {
	return c.runResume
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
func (c *Container) TriggerScheduler() *triggers.Scheduler {
	return c.triggerSched
}

func (c *Container) WorldState() *memory.WorldState {
	return c.worldState
}

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

	deps, err := buildContainerRuntimeDeps(ctx, cfg, store)
	if err != nil {
		return nil, err
	}

	wsDir := filepath.Join(strings.TrimSpace(cfg.Runtime.StorageDir), "worldstate")
	ws, err := memory.NewWorldState(wsDir)
	if err != nil {
		return nil, fmt.Errorf("build world state: %w", err)
	}
	deps.worldStateUpdater = &worldStateAdapter{ws: ws}

	container, err := buildContainerAppServices(cfg, store, deps, ws)
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
// Semantic retrieval (embedding + vector store) will be wired in Phase 4.
func buildMemoryService(ctx context.Context, cfg *config.Config) (memory.Service, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	memoryRoot := strings.TrimSpace(cfg.Runtime.StorageDir)
	svc, err := memory.NewLocalService(memory.Config{
		Root: memoryRoot,
	})
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func buildContainerAppServices(cfg *config.Config, db *store.Store, deps *containerRuntimeDeps, ws *memory.WorldState) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.runResume = api.NewRunResumeService(db).WithResume(deps.resumeRun)
	container.skills = api.NewSkillService(cfg, deps.loader)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.threads = api.NewThreadService(db, workspaceRoot)
	container.runs = api.NewRunService(db, container.threads, deps.executeRun, deps.runController)
	container.events = api.NewEventService(db, db)
	container.pendingAction = api.NewPendingActionService(db)

	container.worldState = ws
	container.capabilities = api.NewCapabilitiesService(cfg, container.skills.Snapshot, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = api.NewDeviceAuthService(db)
	container.inbox = api.NewInboxService(db, container.capabilities)

	container.triggerSched = buildTriggerScheduler(cfg, container.runs, db, ws)

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

type triggerRunCreator struct {
	runs            *api.RunService
	store           core.SessionStore
	worldState      *memory.WorldState
	mu              sync.Mutex
	lastFingerprint string
}

func (t *triggerRunCreator) CreateRun(ctx context.Context, triggerID, input string) error {
	threadID := "trigger:" + triggerID
	if _, err := t.store.LoadSession(ctx, threadID); err != nil {
		if _, cerr := t.store.CreateSession(ctx, threadID, "Trigger: "+triggerID); cerr != nil {
			return cerr
		}
	}
	// Skip duplicate fires: same WorldState + same input = same response.
	// Saves an LLM call when a webhook re-fires with no state change.
	if t.shouldSkipRun(t.worldState, input) {
		slog.Info("trigger fire skipped (duplicate of last fire)", "trigger_id", triggerID)
		return nil
	}
	input = injectWorldState(ctx, t.worldState, input)
	if _, err := t.runs.CreateRun(ctx, threadID, "", input); err != nil {
		return err
	}
	return nil
}

// shouldSkipRun reports whether this fire is a duplicate of the last one:
// same WorldState projection + same input. If so, the agent would produce
// the same response — skip the LLM call to save cost.
func (t *triggerRunCreator) shouldSkipRun(ws *memory.WorldState, input string) bool {
	if ws == nil {
		return false
	}
	fp := computeFireFingerprint(ws, input)
	t.mu.Lock()
	defer t.mu.Unlock()
	if fp == t.lastFingerprint {
		return true
	}
	t.lastFingerprint = fp
	return false
}

func computeFireFingerprint(ws *memory.WorldState, input string) string {
	projection, err := ws.Load(context.Background())
	if err != nil {
		projection = nil
	}
	h := sha256.New()
	h.Write([]byte(input))
	// Sort keys for deterministic output — map iteration order is random.
	keys := make([]string, 0, len(projection))
	for k := range projection {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(projection[k]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// injectWorldState prepends the current WorldState projection to the run
// input. If WorldState is nil, Load fails, or the projection is empty, the
// input is returned unchanged. On Load error it logs a warning and proceeds
// without the projection — a stale projection is better than blocking a
// trigger fire.
func injectWorldState(ctx context.Context, ws *memory.WorldState, input string) string {
	if ws == nil {
		return input
	}
	projection, err := ws.Load(ctx)
	if err != nil {
		slog.Warn("world state load failed for trigger, proceeding without projection", "error", err)
		return input
	}
	if len(projection) == 0 {
		return input
	}
	return formatWorldStatePrefix(projection) + input
}

// formatWorldStatePrefix renders the WorldState key-values as a system-style
// context block prepended to the trigger input.
func formatWorldStatePrefix(projection map[string]string) string {
	var b strings.Builder
	b.WriteString("[Current world state projection]\n")
	for k, v := range projection {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\n")
	}
	b.WriteString("[End world state]\n\n")
	return b.String()
}

func buildTriggerScheduler(cfg *config.Config, runs *api.RunService, db *store.Store, ws *memory.WorldState) *triggers.Scheduler {
	var opts []triggers.SchedulerOption
	if cfg.Triggers.DebounceMillis > 0 {
		opts = append(opts, triggers.WithDebounce(time.Duration(cfg.Triggers.DebounceMillis)*time.Millisecond))
	}
	sched := triggers.NewScheduler(&triggerRunCreator{runs: runs, store: db, worldState: ws}, opts...)
	for _, wh := range cfg.Triggers.Webhooks {
		wt, err := triggers.NewWebhookTrigger(triggers.WebhookConfig{
			ID:     wh.ID,
			Secret: wh.Secret,
			Prompt: wh.Prompt,
		})
		if err != nil {
			slog.Warn("skipping webhook trigger", "id", wh.ID, "error", err)
			continue
		}
		sched.Register(wt)
	}
	return sched
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
