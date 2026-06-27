package core

import (
	"errors"
	"time"

	"github.com/cloudwego/eino/adk"
)

// --- Sentinel errors ---

var (
	ErrRunNotActive      = errors.New("run not active")
	ErrRunNotInterrupted = errors.New("run not interrupted")
	ErrExecutionNotReady = errors.New("execution not ready")
)
var (
	ErrRunNotFound              = errors.New("run not found")
	ErrSessionNotFound          = errors.New("session not found")
	ErrSessionMessageNotFound   = errors.New("session message not found")
	ErrFactNotFound             = errors.New("fact not found")
	ErrPendingActionNotFound    = errors.New("pending action not found")
	ErrPendingActionExists      = errors.New("pending action already exists")
	ErrPendingActionDecided     = errors.New("pending action already decided")
	ErrUnsupportedStorageSchema = errors.New("unsupported storage schema")
	ErrOAuthTokenNotFound       = errors.New("oauth token not found")
	ErrDeviceNotFound           = errors.New("device not found")
	ErrPairingCodeNotFound      = errors.New("pairing code not found")
	ErrPairingCodeUsed          = errors.New("pairing code already used")
	ErrPairingCodeExpired       = errors.New("pairing code expired")
	ErrArtifactNotFound         = errors.New("artifact not found")
)

type RunStatus string

const (
	RunStatusRunning     RunStatus = "running"
	RunStatusSucceeded   RunStatus = "succeeded"
	RunStatusInterrupted RunStatus = "interrupted"
	RunStatusFailed      RunStatus = "failed"
)

// --- Run / event / session records ---

type RunRecord struct {
	RunID      string    `json:"run_id"`
	SessionID  string    `json:"session_id,omitempty"`
	TurnIndex  int       `json:"turn_index,omitempty"`
	Status     RunStatus `json:"status"`
	Input      string    `json:"input"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type EventRecord struct {
	Sequence  int64     `json:"sequence"`
	RunID     string    `json:"run_id"`
	Kind      string    `json:"kind"`
	Payload   any       `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionRecord struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EventKind string

// --- Pending actions ---

type PendingActionKind string

const (
	PendingActionKindElicitation      PendingActionKind = "elicitation"
	PendingActionKindOperatorQuestion PendingActionKind = "operator_question"
)

type PendingActionStatus string

const (
	PendingActionStatusPending  PendingActionStatus = "pending"
	PendingActionStatusApproved PendingActionStatus = "approved"
	PendingActionStatusRejected PendingActionStatus = "rejected"
	PendingActionStatusResolved PendingActionStatus = "resolved"
)

type PendingActionRecord struct {
	ActionID     string              `json:"action_id"`
	RunID        string              `json:"run_id"`
	InterruptID  string              `json:"interrupt_id,omitempty"`
	Kind         PendingActionKind   `json:"kind"`
	Subject      string              `json:"subject,omitempty"`
	PayloadJSON  string              `json:"payload_json"`
	Status       PendingActionStatus `json:"status"`
	Reason       string              `json:"reason,omitempty"`
	DecisionJSON string              `json:"decision_json,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	ResolvedAt   *time.Time          `json:"resolved_at,omitempty"`
}

// --- Operator question ---

const (
	OperatorQuestionDecisionAnswer  = "answer"
	OperatorQuestionDecisionDecline = "decline"
)

type PendingActionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type OperatorQuestionPayload struct {
	Question      string                `json:"question"`
	Options       []PendingActionOption `json:"options,omitempty"`
	AllowFreeform bool                  `json:"allow_freeform,omitempty"`

	// Decision Card fields (ADR-0001 #4). These are optional; old payloads
	// without them still decode. When present, mobile renders a Decision Card
	// (options + evidence + rationale + risk) instead of a bare question.
	ConsideredOptions []ConsideredOption `json:"considered_options,omitempty"`
	Rationale         string             `json:"rationale,omitempty"`
	Risk              string             `json:"risk,omitempty"`
	Recommendation    string             `json:"recommendation,omitempty"`
}

// ConsideredOption is one alternative the agent evaluated before asking for
// approval. RejectedReason is empty for the recommended option.
type ConsideredOption struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Evidence       string `json:"evidence,omitempty"`
	RejectedReason string `json:"rejected_reason,omitempty"`
}

type OperatorQuestionDecision struct {
	Action           string `json:"action"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

// --- ExecuteRequest ---

type ExecuteRequest struct {
	RunID            string
	SessionID        string
	TurnIndex        int
	Input            string
	BoundMessageID   int64
	SkillID          string
	AllowedToolNames []string
	Messages         []adk.Message
}
