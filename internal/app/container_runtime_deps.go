package app

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

type containerRuntimeDeps struct {
	ws                    *workspace.Workspace
	loader                *skills.Loader
	checkpointService     *workingstate.Service
	sessionSummaryService *model.SessionSummaryService
	memoryModule          memorymodule.Service
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
	var checkpointService *workingstate.Service
	sessionSummaryService := model.NewSessionSummaryService(store, 2000)
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
		CheckpointService:     checkpointService,
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
		checkpointService:     checkpointService,
		sessionSummaryService: sessionSummaryService,
		memoryModule:          memoryModule,
		contextPlane:          contextPlane,
		mcpPendingActionStore: mcpPendingActionStore,
		runnerFactory:         runnerFactory,
		runController:         runController,
		executors:             executors,
	}, nil
}
