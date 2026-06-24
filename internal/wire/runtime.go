package wire

import (
	"context"
	"errors"
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

// api.StoreView is the store contract required by the app-facing services
// (client, inbox, pending-action, run-resume). The previously narrow
// sessionStore/runResumeStore/clientStore/deviceAuthStore/inboxStore
// interfaces are inlined here (they were only embedded, never used standalone
// except as service dependencies which now depend on this wider composite),
// collapsing the consumer-owned port surface. This is an intentional trade-off
// (doneCriteria #10): ISP regression is accepted in exchange for consolidating
// consumer-owned store interfaces to <=4, enforced by
// store_interface_count_test.go.
// api.StoreView moved to api package

type containerRuntimeDeps struct {
	ws                    *workspace.Workspace
	loader                *skills.Loader
	sessionSummaryService *core.SessionSummaryService
	memoryModule          memory.Service
	contextPlane          runtime.Plane
	mcpPendingActionStore api.StoreView
	toolRegistry          core.ToolRegistry
	runnerFactory         *runtime.RunnerFactory
	runController         *runtime.RunController
	executors             func(context.Context) (api.ExecutorHandle, error)
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, db *store.Store) (*containerRuntimeDeps, error) {
	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	sessionSummaryService := core.NewSessionSummaryService(db, 2000)
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

	toolRegistry := tools.NewToolRegistry()
	if err := tools.RegisterNativeTools(toolRegistry, tools.CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   !cfg.Tools.Mutation.Disabled,
		RunCommandEnabled: !cfg.Tools.RunCommand.Disabled,
		ArtifactService:   artifactSvc,
		OperatorStore:     mcpPendingActionStore,
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

// api.ExecutorHandle moved to api package

type runtimeExecutorHandle struct {
	exec *runtime.Executor
}

func newExecutorFactory(cfg *config.Config, store runtime.ExecutorStore, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (api.ExecutorHandle, error) {
	return func(_ context.Context) (api.ExecutorHandle, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, controller)
		if err != nil {
			return nil, err
		}
		return runtimeExecutorHandle{exec: exec}, nil
	}
}

func (h runtimeExecutorHandle) ExecuteMessages(ctx context.Context, req core.ExecuteRequest, observer api.RunStartObserver) error {
	result, err := h.exec.ExecuteMessages(ctx, req, streamSinkForRunStart(observer))
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("runtime executor returned nil result")
	}
	return nil
}

func (h runtimeExecutorHandle) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*api.ExecutorRunResult, error) {
	result, err := h.exec.ResumeWithTargets(ctx, runID, targets, nil)
	if err != nil {
		return nil, err
	}
	return executorRunResultFromRuntime(result)
}

func streamSinkForRunStart(observer api.RunStartObserver) core.StreamSink {
	if observer == nil {
		return nil
	}
	return func(item core.StreamItem) error {
		if item.Kind == core.StreamKindRunStarted {
			observer.RunStarted()
		}
		return nil
	}
}

func executorRunResultFromRuntime(result *runtime.Result) (*api.ExecutorRunResult, error) {
	if result == nil {
		return nil, errors.New("runtime executor returned nil result")
	}
	return &api.ExecutorRunResult{
		RunID:       result.RunID,
		Status:      result.Status,
		Output:      result.Output,
		Error:       result.Error,
		Interrupted: result.Interrupted,
	}, nil
}
