package app

import (
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
)

type Thread struct {
	ID            string
	Title         string
	WorkspaceRoot string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LatestRunID   string
	State         string
}

type MessageContent struct {
	Type  string
	Text  string
	Parts []MessagePart
}

type Message struct {
	ID        string
	ThreadID  string
	Role      string
	Content   MessageContent
	CreatedAt time.Time
	RunID     string
}

type MessagePart struct {
	Kind             string           `json:"kind"`
	Text             string           `json:"text,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Status           string           `json:"status,omitempty"`
	Title            string           `json:"title,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Changed          []string         `json:"changed,omitempty"`
	Verified         []string         `json:"verified,omitempty"`
	Risks            []string         `json:"risks,omitempty"`
	Items            []DisclosureItem `json:"items,omitempty"`
	DetailRunID      string           `json:"detail_run_id,omitempty"`
	RunID            string           `json:"run_id,omitempty"`
	Label            string           `json:"label,omitempty"`
	DecisionID       string           `json:"decision_id,omitempty"`
	Question         string           `json:"question,omitempty"`
	SelectedOptionID string           `json:"selected_option_id,omitempty"`
	Answer           string           `json:"answer,omitempty"`
	Options          []DecisionOption `json:"options,omitempty"`
	Action           *MessageAction   `json:"action,omitempty"`
}

type DisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type MessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

type Run struct {
	ID          string
	ThreadID    string
	Status      string
	Mode        string
	CreatedAt   time.Time
	CompletedAt time.Time
}

type RunStartedData struct {
	Input string `json:"input,omitempty"`
}

type AssistantDeltaData struct {
	AssistantDelta map[string]any `json:"assistant_delta"`
}

type AgentMessageData struct {
	Message map[string]any `json:"message"`
}

type ToolCallStartedData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallProgressData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallSucceededData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallFailedData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallInterruptedData struct {
	ToolCall map[string]any `json:"tool_call"`
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

type DecisionSelectedData struct {
	Action              string `json:"action,omitempty"`
	Intent              string `json:"intent,omitempty"`
	SelectedSkillID     string `json:"selected_skill_id,omitempty"`
	DecisionReason      string `json:"decision_reason,omitempty"`
	DecisionProfileHash string `json:"decision_profile_hash,omitempty"`
	ExplicitSkillID     string `json:"explicit_skill_id,omitempty"`
}

type DecisionBlockedData = DecisionSelectedData

type SkillData struct {
	Skill map[string]any `json:"skill,omitempty"`
}

type SkillLifecycleData struct {
	SkillLifecycle map[string]any `json:"skill_lifecycle"`
}

type ProcedureActivationData struct {
	ProcedureActivation map[string]any `json:"procedure_activation"`
}

type MemoryPreparedData struct {
	MemoryPrepared map[string]any `json:"memory_prepared"`
}

type ContextPressureData struct {
	ContextPressure map[string]any `json:"context_pressure"`
}

type CrystallizationVerdictData struct {
	RunID     string `json:"run_id"`
	Verdict   string `json:"verdict"`
	SkillID   string `json:"skill_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
	SimilarTo string `json:"similar_to,omitempty"`
}

type CrystallizationFailedData struct {
	RunID string `json:"run_id"`
	Error string `json:"error,omitempty"`
}

type ContextCompressedData struct {
	ContextCompressed map[string]any `json:"context_compressed"`
}

type PlanData struct {
	Plan map[string]any `json:"plan,omitempty"`
}

type PlanClearedData struct {
	PlanID string `json:"plan_id,omitempty"`
}

type PlanStepData struct {
	PlanID    string         `json:"plan_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Plan      map[string]any `json:"plan,omitempty"`
	Step      map[string]any `json:"step,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type SubagentData struct {
	SubRunID          string   `json:"sub_run_id,omitempty"`
	ParentID          string   `json:"parent_id,omitempty"`
	SessionID         string   `json:"session_id,omitempty"`
	Depth             int      `json:"depth,omitempty"`
	Task              string   `json:"task,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	FinalStatus       string   `json:"final_status,omitempty"`
	AcceptanceStatus  string   `json:"acceptance_status,omitempty"`
	AcceptanceReasons []string `json:"acceptance_reasons,omitempty"`
	OrchestrationMode string   `json:"orchestration_mode,omitempty"`
	ParentStepID      string   `json:"parent_step_id,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type RunEvent struct {
	EventID string
	RunID   string
	Seq     int64
	TS      time.Time
	Type    string
	Data    any
}

type UnsupportedRunEvent struct {
	EventID string
	RunID   string
	Seq     int64
	TS      time.Time
	Type    string
	Raw     map[string]any
	Reason  string
}

type RunEventDetail struct {
	Events      []RunEvent
	Unsupported []UnsupportedRunEvent
	Trace       *runtime.TraceSummary
}
