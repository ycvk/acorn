package clientevents

import (
	"time"

	"github.com/ycvk/acorn/internal/events"
)

// RunEvent is the client-visible live event envelope used by /v1 run detail and SSE.
type RunEvent struct {
	EventID string    `json:"event_id"`
	RunID   string    `json:"run_id"`
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
}

// RunEventBatch is a live event page plus the persisted cursor scanned to build it.
type RunEventBatch struct {
	Events    []RunEvent
	CursorSeq int64
}

// RunEventDetail aggregates client-visible live events for a run detail view.
type RunEventDetail struct {
	Events []RunEvent
}

type SessionState string

const (
	SessionStateNew         SessionState = "new"
	SessionStateRunning     SessionState = "running"
	SessionStateCompleted   SessionState = "completed"
	SessionStateFailed      SessionState = "failed"
	SessionStateInterrupted SessionState = "interrupted"
	SessionStateDegraded    SessionState = "degraded"
)

// --- Event payload data types ---

type RunStartedData struct {
	Input string `json:"input,omitempty"`
}

type AssistantDeltaData struct {
	AssistantDelta map[string]any `json:"assistant_delta"`
}

type AgentMessageData struct {
	Message map[string]any `json:"message"`
}

type RunCompletedData struct {
	Message map[string]any `json:"message,omitempty"`
}

type RunFailedData struct {
	Error string `json:"error,omitempty"`
}

type RunInterruptedData struct {
	Interrupt map[string]any `json:"interrupt,omitempty"`
}

type RunResumeRequestedData struct {
	Targets map[string]any `json:"targets,omitempty"`
}

type ElicitationPendingData struct {
	ActionID        string `json:"action_id"`
	Message         string `json:"message,omitempty"`
	RequestedSchema any    `json:"requested_schema,omitempty"`
}

type ElicitationDecidedData = ElicitationPendingData

type OperatorQuestionData struct {
	ActionID         string                       `json:"action_id"`
	Question         string                       `json:"question,omitempty"`
	Options          []events.PendingActionOption `json:"options,omitempty"`
	AllowFreeform    bool                         `json:"allow_freeform,omitempty"`
	Decision         string                       `json:"decision,omitempty"`
	SelectedOptionID string                       `json:"selected_option_id,omitempty"`
	Answer           string                       `json:"answer,omitempty"`
}

type OperatorQuestionPendingData = OperatorQuestionData
type OperatorQuestionDecidedData = OperatorQuestionData

type DecisionBlockedData struct {
	Action              string `json:"action,omitempty"`
	Intent              string `json:"intent,omitempty"`
	SelectedSkillID     string `json:"selected_skill_id,omitempty"`
	DecisionReason      string `json:"decision_reason,omitempty"`
	DecisionProfileHash string `json:"decision_profile_hash,omitempty"`
	ExplicitSkillID     string `json:"explicit_skill_id,omitempty"`
}
