package app

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtimehistory"
	storecore "github.com/ycvk/acorn/internal/store"
)

type sessionStore interface {
	CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error)
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error)
}

type pendingActionDecisionStore interface {
	ListPendingActions(ctx context.Context, limit int) ([]events.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status events.PendingActionStatus, mode events.PendingActionDecisionMode, decisionJSON string) (*events.PendingActionRecord, error)
	SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

type traceStore interface {
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
}

type decisionStore interface {
	LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error)
}

type sessionStateStore interface {
	LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error)
	GetSessionSummary(ctx context.Context, sessionID string) (*runtimehistory.SessionSummary, error)
}

type clientStore interface {
	ListSessions(ctx context.Context, limit int) ([]events.SessionRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*events.RunRecord, error)
	CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(sessionID string, turnIndex int, role, content, runID string) (*events.SessionMessageRecord, error)
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*events.SessionMessageRecord, error)
	FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

type inboxStore interface {
	ListPendingActions(ctx context.Context, limit int) ([]events.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	ListActiveRuns(ctx context.Context, limit int) ([]events.RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]events.RunRecord, error)
}

type deviceAuthStore interface {
	SavePairingCode(ctx context.Context, code *storecore.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*storecore.PairingCode, error)
	SaveDevice(ctx context.Context, device *storecore.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*storecore.Device, error)
	ListDevices(ctx context.Context) ([]storecore.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

type notificationStore interface {
	UpsertDevicePushToken(ctx context.Context, token *storecore.DevicePushToken) (*storecore.DevicePushToken, error)
	LoadDevicePushToken(ctx context.Context, deviceID, provider string) (*storecore.DevicePushToken, error)
	RevokeDevicePushToken(ctx context.Context, deviceID, provider string, revokedAt time.Time) error
	ListActiveDevicePushTokens(ctx context.Context) ([]storecore.DevicePushToken, error)
	CreateNotification(ctx context.Context, notification *storecore.Notification) error
	CreateNotificationDelivery(ctx context.Context, delivery *storecore.NotificationDelivery) error
	UpdateNotificationDeliveryStatus(ctx context.Context, deliveryID, status, errorText string, updatedAt time.Time) error
}
