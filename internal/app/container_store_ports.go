package app

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime"
	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workingstate"
)

// containerRuntimeStore is the store contract required by the runtime container
// wiring. It composes RunnerFactoryStore with the context-plane store, working
// state, session-summary, and pending-action-create ports.
type containerRuntimeStore interface {
	runtime.RunnerFactoryStore
	workingstate.Store
	model.SessionSummaryStore
	PendingActionCreateStore
}

// containerAppStore is the store contract required by the app-facing services
// (client, inbox, pending-action, run-resume, decision). The previously narrow
// sessionStore/runResumeStore/clientStore/pendingActionDecisionStore/
// decisionStore/deviceAuthStore/inboxStore interfaces are inlined here (they
// were only embedded, never used standalone except as service dependencies which
// now depend on this wider composite), collapsing the consumer-owned port
// surface. This is an intentional trade-off (doneCriteria #10): ISP regression
// is accepted in exchange for consolidating consumer-owned store interfaces
// to <=6, enforced by store_interface_count_test.go.
type containerAppStore interface {
	CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]events.SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*events.RunRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*events.SessionMessageRecord, error)
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]storecore.ArtifactRecord, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*events.SessionMessageRecord, error)
	FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]events.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status events.PendingActionStatus, mode events.PendingActionDecisionMode, decisionJSON string) (*events.PendingActionRecord, error)
	SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error
	ListActiveRuns(ctx context.Context, limit int) ([]events.RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]events.RunRecord, error)
	LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error)
	SavePairingCode(ctx context.Context, code *storecore.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*storecore.PairingCode, error)
	SaveDevice(ctx context.Context, device *storecore.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*storecore.Device, error)
	ListDevices(ctx context.Context) ([]storecore.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}
