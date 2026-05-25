package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	notificationrouter "github.com/ycvk/acorn/internal/notifications"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/toolfactory"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

type WorkingCheckpointService struct {
	service *workingstate.Service
}

func NewWorkingCheckpointService(service *workingstate.Service) (*WorkingCheckpointService, error) {
	if service == nil {
		return nil, errors.New("working checkpoint service is required")
	}
	return &WorkingCheckpointService{service: service}, nil
}

func (s *WorkingCheckpointService) Get(ctx context.Context, threadID string) (*WorkingCheckpointView, error) {
	checkpoint, err := s.service.Get(ctx, threadID)
	if err != nil || checkpoint == nil {
		return nil, err
	}
	return checkpointViewFromDomain(*checkpoint), nil
}

func (s *WorkingCheckpointService) Update(ctx context.Context, threadID, content, relatedSkillID string) (*WorkingCheckpointView, error) {
	checkpoint, err := s.service.Update(ctx, threadID, content, relatedSkillID)
	if err != nil || checkpoint == nil {
		return nil, err
	}
	return checkpointViewFromDomain(*checkpoint), nil
}

func (s *WorkingCheckpointService) Clear(ctx context.Context, threadID string) error {
	return s.service.Clear(ctx, threadID)
}

type WorkingCheckpointView struct {
	ThreadID       string    `json:"thread_id"`
	Content        string    `json:"content"`
	RelatedSkillID string    `json:"related_skill_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func checkpointViewFromDomain(checkpoint workingstate.Checkpoint) *WorkingCheckpointView {
	return &WorkingCheckpointView{
		ThreadID:       checkpoint.ThreadID,
		Content:        checkpoint.Content,
		RelatedSkillID: checkpoint.RelatedSkillID,
		UpdatedAt:      checkpoint.UpdatedAt,
	}
}

func environmentMap() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func localEligibilityToolNames(cfg *config.Config) []string {
	specs := tooling.ConfiguredLocalSpecs(cfg)
	names := make([]string, 0, len(specs)+11)
	seen := make(map[string]struct{}, len(specs)+11)
	for _, spec := range specs {
		if spec.Enabled() {
			if _, ok := seen[spec.Name]; ok {
				continue
			}
			seen[spec.Name] = struct{}{}
			names = append(names, spec.Name)
		}
	}
	for _, name := range tooling.BuiltinToolNames() {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func staticSkillEligibilityContext(cfg *config.Config) skills.EligibilityContext {
	availableTools := localEligibilityToolNames(cfg)
	availableToolsets := make([]string, 0, 1)
	if cfg != nil {
		for _, provider := range cfg.MCP.Providers {
			if !provider.Enabled {
				continue
			}
			availableToolsets = append(availableToolsets, provider.Name)
			availableTools = append(availableTools, provider.ToolNames...)
		}
	}
	return skills.EligibilityContext{
		AvailableTools:    availableTools,
		AvailableToolsets: availableToolsets,
		Env:               environmentMap(),
	}
}

type executorHandle interface {
	Run(ctx context.Context, input, skillID string, sink runtime.StreamSink) (*runtime.Result, error)
	ExecuteMessages(ctx context.Context, req runtime.ExecuteRequest, sink runtime.StreamSink) (*runtime.Result, error)
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error)
}

func newExecutorFactory(cfg *config.Config, store *storesqlite.Store, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (executorHandle, error) {
	return func(_ context.Context) (executorHandle, error) {
		return runtime.NewExecutorWithRunnerFactoryAndController(cfg, store, runnerFactory, controller)
	}
}

type Container struct {
	cfg           *config.Config
	store         *storesqlite.Store
	runnerFactory *runtime.RunnerFactory
	runController *runtime.RunController
	sessions      *SessionService
	trace         *TraceService
	sessionState  *SessionStateService
	workbench     *RuntimeWorkbenchService
	checkpoints   *WorkingCheckpointService
	skills        *SkillService
	chat          *ChatService
	client        *ClientService
	pendingAction *PendingActionService
	run           *RunService
	resume        *ResumeService
	decision      *DecisionService
	memory        *MemoryService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
	notifications *NotificationService
	mcpServer     *mcp.Server
	serveToolset  *toolfactory.Toolset
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

func (c *Container) Sessions() *SessionService {
	return c.sessions
}

func (c *Container) SessionState() *SessionStateService {
	return c.sessionState
}

func (c *Container) Workbench() *RuntimeWorkbenchService {
	return c.workbench
}

func (c *Container) Checkpoints() *WorkingCheckpointService {
	return c.checkpoints
}

func (c *Container) Chat() *ChatService {
	return c.chat
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

func (c *Container) Run() *RunService {
	return c.run
}

func (c *Container) Resume() *ResumeService {
	return c.resume
}

func (c *Container) Memory() *MemoryService {
	return c.memory
}

func (c *Container) Decision() *DecisionService {
	return c.decision
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

func (c *Container) Notifications() *NotificationService {
	return c.notifications
}

func (c *Container) MCPServer() *mcp.Server {
	return c.mcpServer
}

func (c *Container) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.serveToolset != nil {
		if err := c.serveToolset.Close(); err != nil {
			errs = append(errs, err)
		}
	}
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

	store, err := storesqlite.Open(cfg.Runtime.StorageDir)
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

	mcpServer, serveToolset, err := buildContainerMCPServer(cfg, deps.runnerFactory)
	if err != nil {
		return nil, err
	}
	container.mcpServer = mcpServer
	container.serveToolset = serveToolset

	committed = true
	return container, nil
}

type containerRuntimeDeps struct {
	ws                     *workspace.Workspace
	loader                 *skills.Loader
	checkpointService      *workingstate.Service
	sessionSummaryService  *runtimehistory.SessionSummaryService
	memoryModule           memorymodule.Service
	semanticIndex          memorymodule.SemanticIndex
	semanticEmbedder       memorymodule.Embedder
	decisionProfileService *decision.ProfileService
	contextPlane           contextplane.ContextPlane
	notificationService    *NotificationService
	mcpPendingActionStore  PendingActionCreateStore
	runnerFactory          *runtime.RunnerFactory
	runController          *runtime.RunController
	executors              func(context.Context) (executorHandle, error)
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, store *storesqlite.Store) (*containerRuntimeDeps, error) {
	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	checkpointService := workingstate.NewService(store, 4000)
	sessionSummaryService := runtimehistory.NewSessionSummaryService(store, 2000)
	memoryModule, semanticIndex, semanticEmbedder, err := buildMemoryModule(ctx, cfg)
	if err != nil {
		return nil, err
	}
	decisionProfileService := decision.NewProfileService(ws.Root())
	if _, err := decisionProfileService.Load(); err != nil {
		return nil, err
	}

	contextPlane, err := buildContextPlane(cfg, store, checkpointService, sessionSummaryService)
	if err != nil {
		return nil, err
	}

	notificationService := NewNotificationService(store, notificationrouter.Router{})
	mcpPendingActionStore := NewNotifyingPendingActionStore(store, notificationService)

	runnerFactory, err := runtime.NewRunnerFactory(cfg, store, runtime.RunnerFactoryOptions{
		Loader:                 loader,
		Workspace:              ws,
		DecisionProfileService: decisionProfileService,
		CheckpointService:      checkpointService,
		SessionSummaryService:  sessionSummaryService,
		MemoryModule:           memoryModule,
		ContextPlane:           contextPlane,
		MCPPendingActionStore:  mcpPendingActionStore,
	})
	if err != nil {
		return nil, fmt.Errorf("init runner factory: %w", err)
	}
	runController := runtime.NewRunController()
	executors := newExecutorFactory(cfg, store, runnerFactory, runController)

	return &containerRuntimeDeps{
		ws:                     ws,
		loader:                 loader,
		checkpointService:      checkpointService,
		sessionSummaryService:  sessionSummaryService,
		memoryModule:           memoryModule,
		semanticIndex:          semanticIndex,
		semanticEmbedder:       semanticEmbedder,
		decisionProfileService: decisionProfileService,
		contextPlane:           contextPlane,
		notificationService:    notificationService,
		mcpPendingActionStore:  mcpPendingActionStore,
		runnerFactory:          runnerFactory,
		runController:          runController,
		executors:              executors,
	}, nil
}

func buildContainerAppServices(cfg *config.Config, store *storesqlite.Store, deps *containerRuntimeDeps) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		store:         store,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.sessions = NewSessionService(store)
	container.trace = NewTraceService(store)
	container.sessionState = NewSessionStateService(cfg, store, container.trace)
	container.workbench = NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{
		Workspace: deps.ws,
	}, store, container.trace)
	checkpoints, err := NewWorkingCheckpointService(deps.checkpointService)
	if err != nil {
		return nil, err
	}
	container.checkpoints = checkpoints
	container.skills = NewSkillService(cfg, deps.loader)
	container.chat = NewChatService(store, deps.executors)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.client = BuildClientService(store, deps.executors, workspaceRoot)
	container.pendingAction = NewPendingActionService(store)
	container.run = NewRunService(deps.executors, deps.runController)
	container.resume = NewResumeService(container.trace, deps.executors, store)
	container.decision = NewDecisionService(deps.decisionProfileService, store)

	memoryService, err := NewMemoryService(deps.memoryModule, MemoryServiceSemanticOptions{
		Index:      deps.semanticIndex,
		Embedder:   deps.semanticEmbedder,
		Model:      cfg.Memory.Semantic.Embedding.Model,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
	})
	if err != nil {
		return nil, err
	}
	container.memory = memoryService

	container.capabilities = NewCapabilitiesService(cfg, container.skills, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = NewDeviceAuthService(store)
	container.inbox = NewInboxService(store, container.capabilities)
	container.notifications = deps.notificationService

	return container, nil
}

func buildMemoryModule(ctx context.Context, cfg *config.Config) (memorymodule.Service, memorymodule.SemanticIndex, memorymodule.Embedder, error) {
	memoryModule, err := memorymodule.NewLocalService(memorymodule.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		return nil, nil, nil, err
	}
	if err := memoryModule.EnsureLayout(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := memoryModule.BuildIndex(ctx); err != nil {
		return nil, nil, nil, err
	}
	semanticIndex, semanticEmbedder, err := buildMemorySemanticDependencies(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if semanticIndex != nil {
		if err := memoryModule.SetSemanticRuntime(memorymodule.SemanticRuntimeOptions{
			Index:      semanticIndex,
			Embedder:   semanticEmbedder,
			Model:      cfg.Memory.Semantic.Embedding.Model,
			Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
			BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
			Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
			IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
			Mode:       "hybrid",
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("set semantic runtime: %w", err)
		}
	}
	return memoryModule, semanticIndex, semanticEmbedder, nil
}

func buildContextPlane(cfg *config.Config, store *storesqlite.Store, checkpointService *workingstate.Service, sessionSummaryService *runtimehistory.SessionSummaryService) (contextplane.ContextPlane, error) {
	contextPolicy, err := cfg.ContextPolicy()
	if err != nil {
		return nil, fmt.Errorf("context policy: %w", err)
	}
	maxContextTokens, err := contextplane.ContextAssemblyTokenLimitFromContextPolicy(contextPolicy)
	if err != nil {
		return nil, fmt.Errorf("context plane budget: %w", err)
	}
	contextCounter, err := contextplane.NewCompressionTokenCounter(contextPolicy)
	if err != nil {
		return nil, fmt.Errorf("context plane token counter: %w", err)
	}
	contextPlane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: cfg.Memory.Search.MemoryContextTokenBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             contextCounter,
		Store:                    store,
		CheckpointService:        checkpointService,
		SessionSummaryService:    sessionSummaryService,
		ToolResultLedger:         store,
		MemoryBudget: contextplane.LayeredMemoryBudget{
			L1IndexTokens:     cfg.Memory.Search.IndexTokenBudget,
			L2InitialTokens:   cfg.Memory.Search.InitialTokenBudget,
			L3OnDemandReserve: cfg.Memory.Search.OnDemandReserve,
		},
	})
	return contextPlane, nil
}

func buildMemorySemanticDependencies(ctx context.Context, cfg *config.Config) (memorymodule.SemanticIndex, memorymodule.Embedder, error) {
	if cfg == nil {
		return nil, nil, errors.New("config is required")
	}
	if err := cfg.ValidateMemorySemanticReady(); err != nil {
		return nil, nil, err
	}
	embedding := cfg.Memory.Semantic.Embedding
	embedder, err := memorymodule.NewOpenAICompatibleEmbedder(memorymodule.OpenAICompatibleEmbedderConfig{
		BaseURL:        embedding.BaseURL,
		APIKey:         embedding.APIKey,
		Model:          embedding.Model,
		Dimensions:     embedding.Dimensions,
		TimeoutSeconds: embedding.TimeoutSeconds,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build semantic embedder: %w", err)
	}
	blevePath := strings.TrimSpace(cfg.Memory.Semantic.Bleve.Path)
	if blevePath == "" {
		blevePath = filepath.Join(cfg.Runtime.StorageDir, "bleve-semantic")
	}
	index, err := memorymodule.NewBleveSemanticIndex(ctx, memorymodule.BleveSemanticIndexConfig{
		Path:       blevePath,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build bleve semantic index: %w", err)
	}
	return index, embedder, nil
}

func buildContainerMCPServer(cfg *config.Config, runnerFactory *runtime.RunnerFactory) (*mcp.Server, *toolfactory.Toolset, error) {
	if len(cfg.Serve.Tools.Allowlist) == 0 {
		return nil, nil, nil
	}
	ctx := context.Background()
	toolset, err := runnerFactory.BuildServeToolset(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("build serve toolset: %w", err)
	}
	mcpServer, err := mcpprovider.NewMCPServer(ctx, cfg.Serve, toolset.All())
	if err != nil {
		if closeErr := toolset.Close(); closeErr != nil {
			return nil, nil, errors.Join(fmt.Errorf("create MCP server: %w", err), fmt.Errorf("close serve toolset after MCP server failure: %w", closeErr))
		}
		return nil, nil, fmt.Errorf("create MCP server: %w", err)
	}
	return mcpServer, toolset, nil
}
