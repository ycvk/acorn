package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

type containerRuntimeDeps struct {
	ws                     *workspace.Workspace
	loader                 *skills.Loader
	checkpointService      *workingstate.Service
	sessionSummaryService  *model.SessionSummaryService
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

type crystallizerFactory func(memorymodule.Service) (crystallization.Service, io.Closer, error)

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, store containerRuntimeStore, buildCrystallizer crystallizerFactory) (*containerRuntimeDeps, error) {
	ws, err := cfg.Workspace()
	if err != nil {
		return nil, err
	}
	loader := skills.NewLoader(cfg)
	checkpointService := workingstate.NewService(store, 4000)
	sessionSummaryService := model.NewSessionSummaryService(store, 2000)
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

	notificationService := NewNotificationService(store, newNotificationRouter(nil))
	mcpPendingActionStore := NewNotifyingPendingActionStore(store, notificationService)

	var crystallizer crystallization.Service
	var crystallizerCloser io.Closer
	if buildCrystallizer != nil {
		crystallizer, crystallizerCloser, err = buildCrystallizer(memoryModule)
		if err != nil {
			return nil, fmt.Errorf("init crystallizer: %w", err)
		}
	}

	runnerFactory, err := runtime.NewRunnerFactory(cfg, store, runtime.RunnerFactoryOptions{
		Loader:                 loader,
		Workspace:              ws,
		DecisionProfileService: decisionProfileService,
		CheckpointService:      checkpointService,
		SessionSummaryService:  sessionSummaryService,
		MemoryModule:           memoryModule,
		ContextPlane:           contextPlane,
		MCPPendingActionStore:  mcpPendingActionStore,
		Crystallizer:           crystallizer,
		CrystallizerCloser:     crystallizerCloser,
	})
	if err != nil {
		if crystallizerCloser != nil {
			err = errors.Join(err, crystallizerCloser.Close())
		}
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
