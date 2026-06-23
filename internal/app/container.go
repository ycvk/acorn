package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workspace"
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

// containerRuntimeStore is the store contract required by the runtime container
// wiring. It composes RunnerFactoryStore with the context-plane store, working
// state, session-summary, and pending-action-create ports.
type containerRuntimeStore interface {
	runtime.RunnerFactoryStore
	domain.SessionSummaryStore
	PendingActionCreateStore
}

// containerAppStore is the store contract required by the app-facing services
// (client, inbox, pending-action, run-resume). The previously narrow
// sessionStore/runResumeStore/clientStore/deviceAuthStore/inboxStore
// interfaces are inlined here (they were only embedded, never used standalone
// except as service dependencies which now depend on this wider composite),
// collapsing the consumer-owned port surface. This is an intentional trade-off
// (doneCriteria #10): ISP regression is accepted in exchange for consolidating
// consumer-owned store interfaces to <=6, enforced by
// store_interface_count_test.go.
type containerAppStore interface {
	CreateSession(ctx context.Context, sessionID, title string) (*domain.SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]domain.SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*domain.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*domain.RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*domain.RunRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]domain.SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*domain.SessionMessageRecord, error)
	LoadRun(ctx context.Context, runID string) (*domain.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]domain.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]domain.EventRecord, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]storecore.ArtifactRecord, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*domain.SessionMessageRecord, error)
	FinishRunContext(ctx context.Context, runID string, status domain.RunStatus, output, errText string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]domain.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*domain.PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status domain.PendingActionStatus, decisionJSON string) (*domain.PendingActionRecord, error)
	ListActiveRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
	SavePairingCode(ctx context.Context, code *storecore.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*storecore.PairingCode, error)
	SaveDevice(ctx context.Context, device *storecore.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*storecore.Device, error)
	ListDevices(ctx context.Context) ([]storecore.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

type containerRuntimeDeps struct {
	ws                    *workspace.Workspace
	loader                *skills.Loader
	sessionSummaryService *domain.SessionSummaryService
	memoryModule          memory.Service
	contextPlane          contextplane.ContextPlane
	mcpPendingActionStore PendingActionCreateStore
	runnerFactory         *runtime.RunnerFactory
	runController         *runtime.RunController
	executors             func(context.Context) (executorHandle, error)
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, store containerRuntimeStore) (*containerRuntimeDeps, error) {
	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	sessionSummaryService := domain.NewSessionSummaryService(store, 2000)
	memoryModule, err := buildMemoryService(ctx, cfg)
	if err != nil {
		return nil, err
	}
	contextPlane, err := buildContextPlane(cfg)
	if err != nil {
		return nil, err
	}

	mcpPendingActionStore := PendingActionCreateStore(store)

	runnerFactory, err := runtime.NewRunnerFactory(cfg, store, runtime.RunnerFactoryOptions{
		Loader:                loader,
		Workspace:             ws,
		SessionSummaryService: sessionSummaryService,
		MemoryModule:          memoryModule,
		ContextPlane:          contextPlane,
		MCPPendingActionStore: mcpPendingActionStore,
	})
	if err != nil {
		return nil, fmt.Errorf("init runner factory: %w", err)
	}
	runController := runtime.NewRunController()
	executors := newExecutorFactory(cfg, store, runnerFactory, runController)

	return &containerRuntimeDeps{
		ws:                    ws,
		loader:                loader,
		sessionSummaryService: sessionSummaryService,
		memoryModule:          memoryModule,
		contextPlane:          contextPlane,
		mcpPendingActionStore: mcpPendingActionStore,
		runnerFactory:         runnerFactory,
		runController:         runController,
		executors:             executors,
	}, nil
}

type executorHandle interface {
	ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer runStartObserver) error
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*executorRunResult, error)
}

type runStartObserver interface {
	RunStarted()
}

type executorRunResult struct {
	RunID       string
	Status      domain.RunStatus
	Output      string
	Error       string
	Interrupted map[string]any
}

type runtimeExecutorHandle struct {
	exec *runtime.Executor
}

func newExecutorFactory(cfg *config.Config, store runtime.ExecutorStore, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (executorHandle, error) {
	return func(_ context.Context) (executorHandle, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, controller)
		if err != nil {
			return nil, err
		}
		return runtimeExecutorHandle{exec: exec}, nil
	}
}

func (h runtimeExecutorHandle) ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer runStartObserver) error {
	result, err := h.exec.ExecuteMessages(ctx, req, streamSinkForRunStart(observer))
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("runtime executor returned nil result")
	}
	return nil
}

func (h runtimeExecutorHandle) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*executorRunResult, error) {
	result, err := h.exec.ResumeWithTargets(ctx, runID, targets, nil)
	if err != nil {
		return nil, err
	}
	return executorRunResultFromRuntime(result)
}

func streamSinkForRunStart(observer runStartObserver) domain.StreamSink {
	if observer == nil {
		return nil
	}
	return func(item domain.StreamItem) error {
		if item.Kind == domain.StreamKindRunStarted {
			observer.RunStarted()
		}
		return nil
	}
}

func executorRunResultFromRuntime(result *runtime.Result) (*executorRunResult, error) {
	if result == nil {
		return nil, errors.New("runtime executor returned nil result")
	}
	return &executorRunResult{
		RunID:       result.RunID,
		Status:      result.Status,
		Output:      result.Output,
		Error:       result.Error,
		Interrupted: cloneMap(result.Interrupted),
	}, nil
}
