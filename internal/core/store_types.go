package core

import (
	"strconv"
	"time"
)

// RunCreateParams holds the parameters for creating a run record.
type RunCreateParams struct {
	RunID          string
	SessionID      string
	TurnIndex      int
	Input          string
	BoundMessageID int64
}

// RunEnd is how a run ends: its status, output and error, and its terminal
// event, which the store records with the status.
type RunEnd struct {
	RunID   string
	Status  RunStatus
	Output  string
	Error   string
	Event   string
	Payload map[string]any
	// Next, when set, is the id of the run the store starts on the owner
	// messages waiting in the conversation, unless another of its runs is
	// going. The terminal event then names it as next_run_id.
	Next string
}

// OwnerMessage is what the owner sends in a conversation: text, files
// uploaded to it, or both. RunID names the run started on it when the
// conversation has none going.
type OwnerMessage struct {
	SessionID string
	Content   string
	FileIDs   []string
	RunID     string
}

// QueuedMessage is an owner message that waits for the conversation's run:
// the run takes it before its next model call, or a run that starts when it
// ends does.
type QueuedMessage struct {
	ID        int64
	SessionID string
	Content   string
	FileIDs   []string
	CreatedAt time.Time
}

// RunStart is a run the store created on owner messages, with the messages
// it took, in the order they were sent.
type RunStart struct {
	RunID     string
	SessionID string
	TurnIndex int
	Input     string
	Messages  []SessionMessageRecord
}

// SentOwnerMessage is what sending an owner message did: RunID is the run
// that takes it. Queued is set when that run was already going and the
// message waits for it; Started is set when the message started the run.
type SentOwnerMessage struct {
	RunID   string
	Queued  *QueuedMessage
	Started *RunStart
}

// PendingActionInput holds the parameters for creating a pending action.
type PendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        PendingActionKind
	Subject     string
	PayloadJSON string
	Reason      string
}

// --- Session messages ---

// MessageRoleWake is the role of a run input written by a commitment wake.
// It reaches the model as a user message; clients show it apart from what the
// owner wrote.
const MessageRoleWake = "wake"

type SessionMessageRecord struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	TurnIndex int       `json:"turn_index"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	RunID     string    `json:"run_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// MessageSourceID names the memory source of a conversation message.
func MessageSourceID(messageID int64) string {
	return "message:" + strconv.FormatInt(messageID, 10)
}

// --- Pairing / devices / OAuth ---

type PairingCode struct {
	CodeHash  string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type Device struct {
	DeviceID   string
	Name       string
	Platform   string
	TokenHash  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

type OAuthToken struct {
	ProviderName string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	UpdatedAt    time.Time
}

// --- Artifacts ---

type ArtifactWriteRequest struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	Content             []byte
	CreatedAt           time.Time
}

type ArtifactRecord struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	RelativePath        string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}

type ArtifactReadRangeRequest struct {
	ArtifactID string
	Offset     int64
	Limit      int64
}

type ArtifactReadRangeResult struct {
	Record  ArtifactRecord
	Offset  int64
	Content []byte
	EOF     bool
}
