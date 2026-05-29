package clientevents

import (
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/skills"
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

// RunEventDetail aggregates live events and trace summary for a run detail view.
type RunEventDetail struct {
	Events []RunEvent
	Trace  *TraceSummary
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

// TraceSummary is the client-visible aggregate of persisted run events.
type TraceSummary struct {
	ItemCount                  int    `json:"item_count"`
	LastKind                   string `json:"last_kind,omitempty"`
	AssistantMessageCount      int    `json:"assistant_message_count,omitempty"`
	AssistantDeltaCount        int    `json:"assistant_delta_count,omitempty"`
	AssistantDeltaMessageCount int    `json:"assistant_delta_message_count,omitempty"`
	AssistantDeltaCharCount    int    `json:"assistant_delta_char_count,omitempty"`
	ToolCallCount              int    `json:"tool_call_count,omitempty"`
	DecisionEventCount         int    `json:"decision_event_count,omitempty"`
	SkillEventCount            int    `json:"skill_event_count,omitempty"`
	PlanEventCount             int    `json:"plan_event_count,omitempty"`
	DecisionSelected           bool   `json:"decision_selected,omitempty"`
	DecisionBlocked            bool   `json:"decision_blocked,omitempty"`
	SkillSelected              bool   `json:"skill_selected,omitempty"`
	Interrupted                bool   `json:"interrupted,omitempty"`
	Failed                     bool   `json:"failed,omitempty"`
	Completed                  bool   `json:"completed,omitempty"`
}

type SelectedSkill struct {
	Skill        skills.Spec
	Score        int
	MatchedTerms []string
}

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
