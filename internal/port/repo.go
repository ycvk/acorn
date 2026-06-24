package port

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/domain"
)

// SessionRepo — 会话 CRUD
type SessionRepo interface {
	CreateSession(ctx context.Context, sessionID, title string) (*domain.SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*domain.SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]domain.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*domain.RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*domain.RunRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error
}

// MessageRepo — 会话消息
type MessageRepo interface {
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]domain.SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*domain.SessionMessageRecord, error)
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*domain.SessionMessageRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status domain.RunStatus) error
}

// RunRepo — 运行记录
type RunRepo interface {
	CreateRun(ctx context.Context, params domain.RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*domain.RunRecord, error)
	FinishRun(ctx context.Context, runID string, status domain.RunStatus, output, errText string) error
	MarkInterrupted(ctx context.Context, runID, output string) error
	UpdateRunOutput(ctx context.Context, runID, output string) error
	ListActiveRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
}

// EventRepo — 事件追加/查询
type EventRepo interface {
	AppendEvent(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]domain.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]domain.EventRecord, error)
}

// PendingActionRepo — 待审批操作
type PendingActionRepo interface {
	CreatePendingAction(ctx context.Context, input domain.PendingActionInput) (*domain.PendingActionRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]domain.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*domain.PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status domain.PendingActionStatus, decisionJSON string) (*domain.PendingActionRecord, error)
}

// DeviceRepo — 设备认证 + 配对
type DeviceRepo interface {
	SavePairingCode(ctx context.Context, code *domain.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*domain.PairingCode, error)
	SaveDevice(ctx context.Context, device *domain.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*domain.Device, error)
	ListDevices(ctx context.Context) ([]domain.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ArtifactRepo — 产物读写
type ArtifactRepo interface {
	WriteArtifact(ctx context.Context, req domain.ArtifactWriteRequest) (domain.ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req domain.ArtifactReadRangeRequest) (domain.ArtifactReadRangeResult, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]domain.ArtifactRecord, error)
	ListArtifactsBySession(ctx context.Context, sessionID string) ([]domain.ArtifactRecord, error)
}

// SummaryRepo — 会话摘要
type SummaryRepo interface {
	SaveSummary(ctx context.Context, sessionID, sourceRunID, runStatus, summary string) error
	LoadSummary(ctx context.Context, sessionID string) (*domain.SessionSummary, error)
}

// OAuthRepo — MCP OAuth token
type OAuthRepo interface {
	SaveOAuthToken(ctx context.Context, token domain.OAuthToken) error
	LoadOAuthToken(ctx context.Context, providerName string) (*domain.OAuthToken, error)
}
