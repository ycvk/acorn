package wire

import (
	"context"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/agent"
	"github.com/ycvk/acorn/internal/config"
	cp "github.com/ycvk/acorn/internal/context"
	"github.com/ycvk/acorn/internal/contract"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workspace"
)

// containerRuntimeStore is the store contract required by the runtime container
// wiring. It composes RunnerFactoryStore with the context-plane store,
// session-summary, and the app-facing contract.StoreView (which subsumes the
// former pending-action-create port).
type containerRuntimeStore interface {
	agent.RunnerFactoryStore
	domain.SessionSummaryStore
	contract.StoreView
}

// contract.StoreView is the store contract required by the app-facing services
// (client, inbox, pending-action, run-resume). The previously narrow
// sessionStore/runResumeStore/clientStore/deviceAuthStore/inboxStore
// interfaces are inlined here (they were only embedded, never used standalone
// except as service dependencies which now depend on this wider composite),
// collapsing the consumer-owned port surface. This is an intentional trade-off
// (doneCriteria #10): ISP regression is accepted in exchange for consolidating
// consumer-owned store interfaces to <=4, enforced by
// store_interface_count_test.go.
// contract.StoreView moved to api package

type containerRuntimeDeps struct {
	ws                    *workspace.Workspace
	loader                *skills.Loader
	sessionSummaryService *domain.SessionSummaryService
	memoryModule          memory.Service
	contextPlane          cp.Plane
	mcpPendingActionStore contract.StoreView
	runnerFactory         *agent.RunnerFactory
	runController         *agent.RunController
	executors             func(context.Context) (contract.ExecutorHandle, error)
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

	mcpPendingActionStore := contract.StoreView(store)

	runnerFactory, err := agent.NewRunnerFactory(cfg, store, agent.RunnerFactoryOptions{
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
	runController := agent.NewRunController()
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

// contract.ExecutorHandle moved to api package

type runtimeExecutorHandle struct {
	exec *agent.Executor
}

func newExecutorFactory(cfg *config.Config, store agent.ExecutorStore, runnerFactory *agent.RunnerFactory, controller *agent.RunController) func(context.Context) (contract.ExecutorHandle, error) {
	return func(_ context.Context) (contract.ExecutorHandle, error) {
		exec, err := agent.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, controller)
		if err != nil {
			return nil, err
		}
		return runtimeExecutorHandle{exec: exec}, nil
	}
}

func (h runtimeExecutorHandle) ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer contract.RunStartObserver) error {
	result, err := h.exec.ExecuteMessages(ctx, req, streamSinkForRunStart(observer))
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("runtime executor returned nil result")
	}
	return nil
}

func (h runtimeExecutorHandle) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*contract.ExecutorRunResult, error) {
	result, err := h.exec.ResumeWithTargets(ctx, runID, targets, nil)
	if err != nil {
		return nil, err
	}
	return executorRunResultFromRuntime(result)
}

func streamSinkForRunStart(observer contract.RunStartObserver) domain.StreamSink {
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

func executorRunResultFromRuntime(result *agent.Result) (*contract.ExecutorRunResult, error) {
	if result == nil {
		return nil, errors.New("runtime executor returned nil result")
	}
	return &contract.ExecutorRunResult{
		RunID:       result.RunID,
		Status:      result.Status,
		Output:      result.Output,
		Error:       result.Error,
		Interrupted: result.Interrupted,
	}, nil
}
