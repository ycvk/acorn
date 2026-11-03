package core

import (
	"errors"
	"time"
)

// --- Sentinel errors ---

var (
	ErrRunNotActive      = errors.New("run not active")
	ErrRunNotInterrupted = errors.New("run not interrupted")
	ErrExecutionNotReady = errors.New("execution not ready")
	// ErrShuttingDown refuses a run that would start while Acorn stops, and is
	// the failure of a run that Acorn stopped.
	ErrShuttingDown = errors.New("acorn is shutting down")
)

// AbandonedRunError is the failure of a run whose process stopped before the
// run ended.
const AbandonedRunError = "Acorn stopped before this run finished"

var (
	ErrRunNotFound              = errors.New("run not found")
	ErrSessionNotFound          = errors.New("session not found")
	ErrThreadBusy               = errors.New("thread has a running run")
	ErrSessionMessageNotFound   = errors.New("session message not found")
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
	FinishedAt time.Time `json:"finished_at"`
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

// --- Pending actions ---

type PendingActionKind string

const (
	PendingActionKindElicitation      PendingActionKind = "elicitation"
	PendingActionKindOperatorQuestion PendingActionKind = "operator_question"
	PendingActionKindToolApproval     PendingActionKind = "tool_approval"
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
}

type OperatorQuestionDecision struct {
	Action           string `json:"action"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

// --- ExecuteRequest ---

type ExecuteRequest struct {
	SourceIDs  []string
	Commitment *CommitmentWake
	Autonomous bool
	// Standalone runs see their input alone, without the thread's history.
	Standalone     bool
	RunID          string
	SessionID      string
	TurnIndex      int
	Input          string
	BoundMessageID int64
	// Created runs were created by the store with their messages bound, as
	// when owner messages start one.
	Created bool
	// Wake says what woke this run; empty means an owner message.
	Wake string
}
