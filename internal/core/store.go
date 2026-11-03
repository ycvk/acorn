package core

import (
	"context"
	"time"
)

// SessionStore combines session, message, run, event, and pending-action
// persistence into a single capability interface.
type SessionStore interface {
	// --- Sessions ---
	CreateSession(ctx context.Context, sessionID, title string) (*SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]SessionRecord, error)
	// LoadLatestRunForSession and LoadLatestRunsForSessions read the
	// conversation's own runs; the sub-runs they delegate are left out.
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*RunRecord, error)
	LoadLatestRepliesForSessions(ctx context.Context, sessionIDs []string) (map[string]SessionMessageRecord, error)
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	// DeleteThread removes a thread and its runs' records; it fails with
	// ErrThreadBusy while one of them runs.
	DeleteThread(ctx context.Context, sessionID string) error

	// --- Messages ---
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*SessionMessageRecord, error)
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	BindUserMessageRunIDByID(ctx context.Context, messageID int64, runID string) error
	BindLatestUserMessageRunID(ctx context.Context, sessionID string, turnIndex int, runID string) error
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status RunStatus) error

	// --- Owner messages ---
	// SendOwnerMessage queues the message while a run of the conversation
	// is running or waits for the owner, and otherwise starts message.RunID
	// on it, after the messages still queued there.
	SendOwnerMessage(ctx context.Context, message OwnerMessage) (SentOwnerMessage, error)
	// TakeQueuedMessages moves the conversation's queued messages into it as
	// further input of runID, while the run runs, in the order they were sent
	// and up to the first that carries files. Each taken message gets a
	// run.input event.
	TakeQueuedMessages(ctx context.Context, runID string) ([]SessionMessageRecord, error)
	// StartQueuedRun starts runID on the conversation's queued messages when
	// none of its runs is going; it returns nil when none is started.
	StartQueuedRun(ctx context.Context, sessionID, runID string) (*RunStart, error)
	ListQueuedMessages(ctx context.Context, sessionID string) ([]QueuedMessage, error)
	// ListQueuedSessions lists the conversations with queued messages.
	ListQueuedSessions(ctx context.Context) ([]string, error)

	// --- Runs ---
	CreateRun(ctx context.Context, params RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*RunRecord, error)
	// RunWoken reports whether Acorn woke the run itself: its input is a
	// wake message, as for the briefing, the reflections, commitments and
	// triggers.
	RunWoken(ctx context.Context, runID string) (bool, error)
	// FinishRun records the run's end with its terminal event, and with
	// end.Next set starts the next run on the conversation's queued messages.
	FinishRun(ctx context.Context, end RunEnd) (EventRecord, *RunStart, error)
	MarkInterrupted(ctx context.Context, runID, output string) error
	ResumeInterruptedRun(ctx context.Context, runID string) error
	ListInterruptedRuns(ctx context.Context) ([]RunRecord, error)
	UpdateRunOutput(ctx context.Context, runID, output string) error
	ListActiveRuns(ctx context.Context, limit int) ([]RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]RunRecord, error)

	// --- Events ---
	AppendEvent(ctx context.Context, runID, kind string, payload any) (EventRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]EventRecord, error)
	LoadToolCallEvents(ctx context.Context, runIDs []string) (map[string][]EventRecord, error)

	// --- Pending actions ---
	CreatePendingAction(ctx context.Context, input PendingActionInput) (*PendingActionRecord, error)
	ListPendingActions(ctx context.Context, limit int) ([]PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status PendingActionStatus, decisionJSON string) (*PendingActionRecord, error)
	ListPendingActionsByRun(ctx context.Context, runID string) ([]PendingActionRecord, error)

	// --- Agent checkpoints ---
	LoadCheckpoint(ctx context.Context, checkpointID string) ([]byte, bool, error)
	SaveCheckpoint(ctx context.Context, checkpointID string, data []byte) error
	DeleteCheckpoint(ctx context.Context, checkpointID string) error
}

// IdentityStore handles device authentication and pairing codes.
type IdentityStore interface {
	SavePairingCode(ctx context.Context, code *PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*PairingCode, error)
	SaveDevice(ctx context.Context, device *Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*Device, error)
	ListDevices(ctx context.Context) ([]Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ArtifactService is the artifact read/write contract. It is embedded by
// ArtifactStore and consumed directly by tool builders (internal/tools)
// that only need artifact I/O and not summary or OAuth persistence.
type ArtifactService interface {
	WriteArtifact(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error)
	ListByRun(ctx context.Context, runID string) ([]ArtifactRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error)
}

// ArtifactStore is the composite persistence contract for artifact I/O and
// OAuth tokens. It embeds ArtifactService so narrow consumers can depend on
// the smaller interface they need.
type ArtifactStore interface {
	ArtifactService

	// OAuth tokens
	SaveOAuthToken(ctx context.Context, token OAuthToken) error
	GetOAuthToken(ctx context.Context, providerName string) (*OAuthToken, error)
}
