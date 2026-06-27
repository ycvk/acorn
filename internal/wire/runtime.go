package wire

import (
	"context"
	"fmt"
	"path/filepath"

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
	memoryModule          memory.Service
	contextPlane          *runtime.ContextPlane
	mcpPendingActionStore core.SessionStore
	toolRegistry          core.ToolRegistry
	worldStateUpdater     tools.WorldStateUpdater
	runnerFactory         *runtime.RunnerFactory
	runController         *runtime.RunController
	executeRun            func(context.Context, core.ExecuteRequest, core.StreamSink) (*runtime.Result, error)
	resumeRun             func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, db *store.Store) (*containerRuntimeDeps, error) {
	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	memoryModule, err := buildMemoryService(ctx, cfg)
	if err != nil {
		return nil, err
	}
	contextPlane, err := buildContextPlane(cfg)
	if err != nil {
		return nil, err
	}

	var mcpPendingActionStore core.SessionStore = db

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
	reviewer := runtime.NewReviewer(memoryModule, runtime.NewChatModelWithModel(cfg, cfg.Memory.Review.ReviewModel), cfg.Memory.Review.ReviewInterval)
	executeRun := func(ctx context.Context, req core.ExecuteRequest, sink core.StreamSink) (*runtime.Result, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, db, runnerFactory, runController)
		if err != nil {
			return nil, err
		}
		exec.SetReviewer(reviewer)
		return exec.ExecuteMessages(ctx, req, sink)
	}
	resumeRun := func(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*runtime.Result, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, db, runnerFactory, runController)
		if err != nil {
			return nil, err
		}
		exec.SetReviewer(reviewer)
		return exec.ResumeWithTargets(ctx, runID, targets, sink)
	}

	return &containerRuntimeDeps{
		ws:                    ws,
		loader:                loader,
		memoryModule:          memoryModule,
		contextPlane:          contextPlane,
		mcpPendingActionStore: mcpPendingActionStore,
		toolRegistry:          toolRegistry,
		runnerFactory:         runnerFactory,
		runController:         runController,
		executeRun:            executeRun,
		resumeRun:             resumeRun,
	}, nil
}
