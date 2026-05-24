package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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
	"github.com/ycvk/acorn/internal/workingstate"
)

func buildContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

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

	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	checkpointService := workingstate.NewService(store, 4000)
	sessionSummaryService := runtimehistory.NewSessionSummaryService(store, 2000)
	memoryModule, err := memorymodule.NewLocalService(memorymodule.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		return nil, err
	}
	if err := memoryModule.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	if err := memoryModule.BuildIndex(ctx); err != nil {
		return nil, err
	}
	semanticIndex, semanticEmbedder, err := buildMemorySemanticDependencies(ctx, cfg)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("set semantic runtime: %w", err)
		}
	}
	decisionProfileService := decision.NewProfileService(ws.Root())
	if _, err := decisionProfileService.Load(); err != nil {
		return nil, err
	}

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

	container := &Container{
		cfg:           cfg,
		store:         store,
		runnerFactory: runnerFactory,
		runController: runController,
	}

	container.sessions = NewSessionService(store)
	container.trace = NewTraceService(store)
	container.sessionState = NewSessionStateService(cfg, store, container.trace)
	container.workbench = NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{
		Workspace: ws,
	}, store, container.trace)
	checkpoints, err := NewWorkingCheckpointService(checkpointService)
	if err != nil {
		return nil, err
	}
	container.checkpoints = checkpoints
	container.skills = NewSkillService(cfg, loader)
	container.chat = NewChatService(store, executors)
	workspaceRoot := ""
	if ws != nil {
		workspaceRoot = ws.Root()
	}
	container.client = BuildClientService(store, executors, workspaceRoot)
	container.pendingAction = NewPendingActionService(store)
	container.run = NewRunService(executors, runController)
	container.resume = NewResumeService(container.trace, executors, store)
	container.decision = NewDecisionService(decisionProfileService, store)

	memoryService, err := NewMemoryService(memoryModule, MemoryServiceSemanticOptions{
		Index:      semanticIndex,
		Embedder:   semanticEmbedder,
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

	container.capabilities = NewCapabilitiesService(cfg, container.skills, mcpprovider.Doctor, runnerFactory)
	container.deviceAuth = NewDeviceAuthService(store)
	container.inbox = NewInboxService(store, container.capabilities)
	container.notifications = notificationService

	mcpServer, err := buildContainerMCPServer(cfg, runnerFactory)
	if err != nil {
		return nil, err
	}
	container.mcpServer = mcpServer

	committed = true
	return container, nil
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

func buildContainerMCPServer(cfg *config.Config, runnerFactory *runtime.RunnerFactory) (*mcp.Server, error) {
	if len(cfg.Serve.Tools.Allowlist) == 0 {
		return nil, nil
	}
	ctx := context.Background()
	toolset, err := runnerFactory.BuildServeToolset(ctx)
	if err != nil {
		return nil, fmt.Errorf("build serve toolset: %w", err)
	}
	mcpServer, err := mcpprovider.NewMCPServer(ctx, cfg.Serve, toolset.All())
	if err != nil {
		return nil, fmt.Errorf("create MCP server: %w", err)
	}
	return mcpServer, nil
}
