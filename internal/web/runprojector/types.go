package runprojector

import (
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
)

// RunEvent is a client-visible run event with typed data.
type RunEvent struct {
	EventID string
	RunID   string
	Seq     int64
	TS      time.Time
	Type    string
	Data    any
}

// UnsupportedRunEvent captures events the client does not yet understand.
type UnsupportedRunEvent struct {
	EventID string
	RunID   string
	Seq     int64
	TS      time.Time
	Type    string
	Raw     map[string]any
	Reason  string
}

// RunEventDetail aggregates events and trace for a run detail view.
type RunEventDetail struct {
	Events      []RunEvent
	Unsupported []UnsupportedRunEvent
	Trace       *runtime.TraceSummary
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

type ProviderDegradedData struct {
	AffectedProviders []ProviderDegradedEntryData `json:"affected_providers"`
}

type ProviderDegradedEntryData struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Error     string `json:"error,omitempty"`
}

type MCPProviderLifecycleData struct {
	ProviderName string `json:"provider_name"`
	Transport    string `json:"transport,omitempty"`
	Error        string `json:"error,omitempty"`
	AuthStatus   string `json:"auth_status,omitempty"`
}

type SamplingData struct {
	RunID string `json:"run_id"`
	Depth int    `json:"depth"`
	Model string `json:"model,omitempty"`
}

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
