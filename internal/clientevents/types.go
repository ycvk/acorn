package clientevents

import (
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/skills"
)

// RunEvent is the client-visible run event envelope used by /v1 run detail and SSE.
type RunEvent struct {
	EventID string    `json:"event_id"`
	RunID   string    `json:"run_id"`
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
}

// UnsupportedRunEvent captures persisted events outside the client contract for diagnostics.
type UnsupportedRunEvent struct {
	EventID string         `json:"event_id"`
	RunID   string         `json:"run_id"`
	Seq     int64          `json:"seq"`
	TS      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	Raw     map[string]any `json:"raw,omitempty"`
	Reason  string         `json:"reason"`
}

// RunEventDetail aggregates events and trace for a run detail view.
type RunEventDetail struct {
	Events      []RunEvent
	Unsupported []UnsupportedRunEvent
	Trace       *TraceSummary
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
	ChildRunMode      string   `json:"child_run_mode,omitempty"`
	WorkspaceMode     string   `json:"workspace_mode,omitempty"`
	WorktreePath      string   `json:"worktree_path,omitempty"`
	ContextMessages   int      `json:"context_messages,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	FinalStatus       string   `json:"final_status,omitempty"`
	AcceptanceStatus  string   `json:"acceptance_status,omitempty"`
	AcceptanceReasons []string `json:"acceptance_reasons,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	OrchestrationMode string   `json:"orchestration_mode,omitempty"`
	ParentStepID      string   `json:"parent_step_id,omitempty"`
	Error             string   `json:"error,omitempty"`
}
