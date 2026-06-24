package contract

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/domain"
)

// StoreView is the store contract required by app-facing services.
type StoreView interface {
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
	ListArtifactsByRun(ctx context.Context, runID string) ([]domain.ArtifactRecord, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*domain.SessionMessageRecord, error)
	FinishRun(ctx context.Context, runID string, status domain.RunStatus, output, errText string) error
	AppendEvent(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]domain.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*domain.PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status domain.PendingActionStatus, decisionJSON string) (*domain.PendingActionRecord, error)
	CreatePendingAction(ctx context.Context, input domain.PendingActionInput) (*domain.PendingActionRecord, error)
	ListActiveRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
	SavePairingCode(ctx context.Context, code *domain.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*domain.PairingCode, error)
	SaveDevice(ctx context.Context, device *domain.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*domain.Device, error)
	ListDevices(ctx context.Context) ([]domain.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ExecutorHandle is the runtime executor contract for run/resume operations.
type ExecutorHandle interface {
	ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer RunStartObserver) error
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*ExecutorRunResult, error)
}

// RunStartObserver is called when a run starts.
type RunStartObserver interface {
	RunStarted()
}

// ExecutorRunResult is the terminal outcome of an executor run.
type ExecutorRunResult struct {
	RunID       string
	Status      domain.RunStatus
	Output      string
	Error       string
	Interrupted map[string]any
}
