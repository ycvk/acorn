package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/context"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workspace"
)

// containerRuntimeStore is the store contract required by the runtime container
// wiring. It composes RunnerFactoryStore with the context-plane store,
// session-summary, and the app-facing containerAppStore (which subsumes the
// former pending-action-create port).
type containerRuntimeStore interface {
	runtime.RunnerFactoryStore
	domain.SessionSummaryStore
	containerAppStore
}

// containerAppStore is the store contract required by the app-facing services
// (client, inbox, pending-action, run-resume). The previously narrow
// sessionStore/runResumeStore/clientStore/deviceAuthStore/inboxStore
// interfaces are inlined here (they were only embedded, never used standalone
// except as service dependencies which now depend on this wider composite),
// collapsing the consumer-owned port surface. This is an intentional trade-off
// (doneCriteria #10): ISP regression is accepted in exchange for consolidating
// consumer-owned store interfaces to <=4, enforced by
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
	CreatePendingAction(ctx context.Context, input storecore.CreatePendingActionInput) (*domain.PendingActionRecord, error)
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
	mcpPendingActionStore containerAppStore
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

	mcpPendingActionStore := containerAppStore(store)

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
