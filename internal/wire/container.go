package wire

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/store"
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

	container, err := buildContainerAppServices(cfg, store, deps)
	if err != nil {
		return nil, err
	}
	container.store = store

	committed = true
	return container, nil
}

func buildContextPlane(cfg *config.Config) (runtime.Plane, error) {
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

func buildContainerAppServices(cfg *config.Config, store api.StoreView, deps *containerRuntimeDeps) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.runResume = api.NewRunResumeService(store).WithResume(deps.executors)
	container.skills = api.NewSkillService(cfg, deps.loader)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.threads = api.NewThreadService(store, workspaceRoot)
	container.runs = api.NewRunService(store, container.threads, deps.executors, deps.runController)
	container.events = api.NewEventService(store)
	container.pendingAction = api.NewPendingActionService(store)

	container.memory = deps.memoryModule

	container.capabilities = api.NewCapabilitiesService(cfg, container.skills.Snapshot, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = api.NewDeviceAuthService(store)
	container.inbox = api.NewInboxService(store, container.capabilities)

	return container, nil
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
// so any readiness gap (binary built without FAISS, unconfigured embedding,
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
