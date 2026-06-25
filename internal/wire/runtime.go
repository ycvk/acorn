package wire

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

type containerRuntimeDeps struct {
	ws                    *workspace.Workspace
	loader                *skills.Loader
	sessionSummaryService *runtime.SessionSummaryService
	memoryModule          memory.Service
	contextPlane          *runtime.ContextPlane
	mcpPendingActionStore api.StoreView
	toolRegistry          core.ToolRegistry
	runnerFactory         *runtime.RunnerFactory
	runController         *runtime.RunController
	executors             func(context.Context) (*runtime.Executor, error)
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, db *store.Store) (*containerRuntimeDeps, error) {
	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	sessionSummaryService := runtime.NewSessionSummaryService(db, 2000)
	memoryModule, err := buildMemoryService(ctx, cfg)
	if err != nil {
		return nil, err
	}
	contextPlane, err := buildContextPlane(cfg)
	if err != nil {
		return nil, err
	}

	mcpPendingActionStore := api.StoreView(db)

	artifactSvc, err := store.NewArtifactService(
		filepath.Join(cfg.Runtime.StorageDir, "artifacts"),
		db,
	)
	if err != nil {
		return nil, fmt.Errorf("artifact service: %w", err)
	}

	ctxBridge := runtime.NewContextBridge()
	toolRegistry := tools.NewToolRegistry()
	if err := tools.RegisterNativeTools(toolRegistry, tools.CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   !cfg.Tools.Mutation.Disabled,
		RunCommandEnabled: !cfg.Tools.RunCommand.Disabled,
		ArtifactService:   artifactSvc,
		ArtifactContext:   ctxBridge,
		OperatorStore:     mcpPendingActionStore,
		OperatorContext:   ctxBridge,
	}); err != nil {
		return nil, fmt.Errorf("register native tools: %w", err)
	}

	runnerFactory, err := runtime.NewRunnerFactory(cfg, db, runtime.RunnerFactoryOptions{
		Loader:                loader,
		Workspace:             ws,
		SessionSummaryService: sessionSummaryService,
		MemoryModule:          memoryModule,
		ContextPlane:          contextPlane,
		MCPPendingActionStore: mcpPendingActionStore,
		ArtifactService:       artifactSvc,
		ToolRegistry:          toolRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("init runner factory: %w", err)
	}
	runController := runtime.NewRunController()
	executors := newExecutorFactory(cfg, db, runnerFactory, runController)

	return &containerRuntimeDeps{
		ws:                    ws,
		loader:                loader,
		sessionSummaryService: sessionSummaryService,
		memoryModule:          memoryModule,
		contextPlane:          contextPlane,
		mcpPendingActionStore: mcpPendingActionStore,
		toolRegistry:          toolRegistry,
		runnerFactory:         runnerFactory,
		runController:         runController,
		executors:             executors,
	}, nil
}
func newExecutorFactory(cfg *config.Config, store core.SessionStore, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (*runtime.Executor, error) {
	return func(_ context.Context) (*runtime.Executor, error) {
		return runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, controller)
	}
}
