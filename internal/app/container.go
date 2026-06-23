package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

type Container struct {
	cfg           *config.Config
	store         *store.Store
	runnerFactory *runtime.RunnerFactory
	runController *runtime.RunController
	runResume     *RunResumeService
	skills        *SkillService
	client        *ClientService
	pendingAction *PendingActionService
	memory        *MemoryService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
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

func (c *Container) RunResume() *RunResumeService {
	return c.runResume
}

func (c *Container) Client() *ClientService {
	return c.client
}

func (c *Container) PendingAction() *PendingActionService {
	return c.pendingAction
}

func (c *Container) Skills() *SkillService {
	return c.skills
}

func (c *Container) Memory() *MemoryService {
	return c.memory
}

func (c *Container) Capabilities() *CapabilitiesService {
	return c.capabilities
}

func (c *Container) DeviceAuth() *DeviceAuthService {
	return c.deviceAuth
}

func (c *Container) Inbox() *InboxService {
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

func buildContextPlane(cfg *config.Config) (contextplane.ContextPlane, error) {
	contextCounter, err := contextplane.NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("context plane token counter: %w", err)
	}
	maxContextTokens := cfg.Context.WindowTokens - cfg.Context.CompactMarginTokens
	if maxContextTokens <= 0 {
		return nil, fmt.Errorf("context effective window must be positive: window=%d margin=%d", cfg.Context.WindowTokens, cfg.Context.CompactMarginTokens)
	}
	contextPlane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
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
		return nil, fmt.Errorf("build memory service: %w", err)
	}
	return svc, nil
}

func buildContainerAppServices(cfg *config.Config, store containerAppStore, deps *containerRuntimeDeps) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.runResume = NewRunResumeService(store).WithResume(deps.executors)
	container.skills = NewSkillService(cfg, deps.loader)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.client = BuildClientService(store, deps.executors, deps.runController, workspaceRoot)
	container.pendingAction = NewPendingActionService(store)

	memoryService, err := NewMemoryService(deps.memoryModule)
	if err != nil {
		return nil, err
	}
	container.memory = memoryService

	container.capabilities = NewCapabilitiesService(cfg, container.skills, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = NewDeviceAuthService(store)
	container.inbox = NewInboxService(store, container.capabilities)

	return container, nil
}

type MemoryService struct {
	module memory.Service
}

func NewMemoryService(module memory.Service) (*MemoryService, error) {
	if module == nil {
		return nil, errors.New("memory module service is required")
	}
	return &MemoryService{module: module}, nil
}

func (s *MemoryService) ListFacts(ctx context.Context, selection memory.RecordSelection) ([]memory.Record, error) {
	return s.module.ListFacts(ctx, selection)
}

func (s *MemoryService) ListSkills(ctx context.Context, selection memory.RecordSelection) ([]memory.Record, error) {
	return s.module.ListSkills(ctx, selection)
}

func (s *MemoryService) ListHistory(ctx context.Context, selection memory.RecordSelection) ([]memory.Record, error) {
	return s.module.ListHistory(ctx, selection)
}

func (s *MemoryService) Search(ctx context.Context, req memory.SearchRequest) (*memory.SearchResult, error) {
	return s.module.Search(ctx, req)
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
	result, err := exec.ExecuteMessages(ctx, domain.ExecuteRequest{
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
