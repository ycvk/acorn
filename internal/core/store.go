package core

import (
	"context"
	"time"
)

// SessionStore combines session, message, run, event, and pending-action
// persistence into a single capability interface. It replaces the former
// SessionRepo, MessageRepo, RunRepo, EventRepo, PendingActionRepo, and the
// ExecutorStore bind methods.
type SessionStore interface {
	// --- Sessions ---
	CreateSession(ctx context.Context, sessionID, title string) (*SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*RunRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error

	// --- Messages ---
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*SessionMessageRecord, error)
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*SessionMessageRecord, error)
	BindUserMessageRunIDByID(ctx context.Context, messageID int64, runID string) error
	BindLatestUserMessageRunID(ctx context.Context, sessionID string, turnIndex int, runID string) error
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status RunStatus) error

	// --- Runs ---
	CreateRun(ctx context.Context, params RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*RunRecord, error)
	FinishRun(ctx context.Context, runID string, status RunStatus, output, errText string) error
	MarkInterrupted(ctx context.Context, runID, output string) error
	UpdateRunOutput(ctx context.Context, runID, output string) error
	ListActiveRuns(ctx context.Context, limit int) ([]RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]RunRecord, error)

	// --- Events ---
	AppendEvent(ctx context.Context, runID, kind string, payload any) (EventRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]EventRecord, error)

	// --- Pending actions ---
	CreatePendingAction(ctx context.Context, input PendingActionInput) (*PendingActionRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status PendingActionStatus, decisionJSON string) (*PendingActionRecord, error)
}

// IdentityStore handles device authentication and pairing codes.
// It replaces the former DeviceRepo.
type IdentityStore interface {
	SavePairingCode(ctx context.Context, code *PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*PairingCode, error)
	SaveDevice(ctx context.Context, device *Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*Device, error)
	ListDevices(ctx context.Context) ([]Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ArtifactStore handles artifact read/write, session summaries, and OAuth tokens.
// It replaces the former ArtifactRepo, SummaryRepo, and OAuthRepo.
type ArtifactStore interface {
	// Artifacts
	WriteArtifact(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error)
	ListByRun(ctx context.Context, runID string) ([]ArtifactRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error)

	// Session summaries
	GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error)
	UpsertSessionSummary(ctx context.Context, summary SessionSummary) error

	// OAuth tokens
	SaveOAuthToken(ctx context.Context, token OAuthToken) error
	GetOAuthToken(ctx context.Context, providerName string) (*OAuthToken, error)
}
