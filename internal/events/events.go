package events

import (
	"encoding/json"
	"time"
)

type RunStatus string

const (
	RunStatusRunning     RunStatus = "running"
	RunStatusSucceeded   RunStatus = "succeeded"
	RunStatusInterrupted RunStatus = "interrupted"
	RunStatusFailed      RunStatus = "failed"
)

type OrchestrationMode string

const (
	ModeDirectResponse OrchestrationMode = "direct_response"
)

func (m OrchestrationMode) Normalize() OrchestrationMode {
	return m
}

type RunRecord struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id,omitempty"`
	TurnIndex int       `json:"turn_index,omitempty"`
	Status    RunStatus `json:"status"`
	Input     string    `json:"input"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	ActionID     string                    `json:"action_id"`
	RunID        string                    `json:"run_id"`
	InterruptID  string                    `json:"interrupt_id,omitempty"`
	Kind         PendingActionKind         `json:"kind"`
	Subject      string                    `json:"subject,omitempty"`
	PayloadJSON  string                    `json:"payload_json"`
	Status       PendingActionStatus       `json:"status"`
	Reason       string                    `json:"reason,omitempty"`
	DecisionJSON string                    `json:"decision_json,omitempty"`
	CreatedAt    time.Time                 `json:"created_at"`
	DecidedAt    *time.Time                `json:"decided_at,omitempty"`
	ResolvedAt   *time.Time                `json:"resolved_at,omitempty"`
}

type SessionMessageRecord struct {
	ID           int64           `json:"id"`
	SessionID    string          `json:"session_id"`
	TurnIndex    int             `json:"turn_index"`
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	ContentParts json.RawMessage `json:"content_parts,omitempty"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}
