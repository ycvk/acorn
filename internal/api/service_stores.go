package api

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type threadStore interface {
	CreateSession(ctx context.Context, sessionID, title string) (*core.SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*core.SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]core.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*core.RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*core.RunRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]core.SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*core.SessionMessageRecord, error)
}

type runStore interface {
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*core.SessionRecord, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*core.SessionMessageRecord, error)
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]core.SessionMessageRecord, error)
	FinishRun(ctx context.Context, runID string, status core.RunStatus, output, errText string) error
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

type runResumeStore interface {
	LoadPendingAction(ctx context.Context, actionID string) (*core.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]core.EventRecord, error)
}

type eventStore interface {
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]core.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]core.EventRecord, error)
	ListByRun(ctx context.Context, runID string) ([]core.ArtifactRecord, error)
}

type pendingActionStore interface {
	ListPendingActions(ctx context.Context, limit int) ([]core.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*core.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status core.PendingActionStatus, decisionJSON string) (*core.PendingActionRecord, error)
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

type inboxStore interface {
	ListActiveRuns(ctx context.Context, limit int) ([]core.RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]core.RunRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]core.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*core.SessionRecord, error)
}

type deviceAuthStore interface {
	SavePairingCode(ctx context.Context, code *core.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*core.PairingCode, error)
	SaveDevice(ctx context.Context, device *core.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*core.Device, error)
	ListDevices(ctx context.Context) ([]core.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}
