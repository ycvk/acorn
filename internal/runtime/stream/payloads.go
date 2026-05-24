package stream

import (
	"time"

	"github.com/ycvk/acorn/internal/runtime/api"
)

// --- Lifecycle payloads ---

type RunStartedPayload struct {
	Input string `json:"input,omitempty"`
}

func (p RunStartedPayload) StreamKind() StreamItemKind { return StreamKindRunStarted }

type RunCompletedPayload struct {
	Message *StreamMessage `json:"message,omitempty"`
}

func (p RunCompletedPayload) StreamKind() StreamItemKind { return StreamKindRunCompleted }

type RunFailedPayload struct {
	Error string `json:"error,omitempty"`
}

func (p RunFailedPayload) StreamKind() StreamItemKind { return StreamKindRunFailed }

type RunInterruptedPayload struct {
	Interrupt *StreamInterrupt `json:"interrupt,omitempty"`
}

func (p RunInterruptedPayload) StreamKind() StreamItemKind { return StreamKindRunInterrupted }

type RunResumeRequestedPayload struct {
	Targets map[string]any `json:"targets,omitempty"`
}

func (p RunResumeRequestedPayload) StreamKind() StreamItemKind { return StreamKindRunResumeRequested }

type RunArchivedPayload struct {
	RunID            string `json:"run_id"`
	EventsCompressed int    `json:"events_compressed"`
}

func (p RunArchivedPayload) StreamKind() StreamItemKind { return StreamKindRunArchived }

// --- Decision payloads ---

type DecisionSelectedPayload struct {
	Action              string `json:"action,omitempty"`
	Intent              string `json:"intent,omitempty"`
	SelectedSkillID     string `json:"selected_skill_id,omitempty"`
	DecisionReason      string `json:"decision_reason,omitempty"`
	DecisionProfileHash string `json:"decision_profile_hash,omitempty"`
	ExplicitSkillID     string `json:"explicit_skill_id,omitempty"`
}

func (p DecisionSelectedPayload) StreamKind() StreamItemKind { return StreamKindDecisionSelected }

type DecisionBlockedPayload struct {
	Action              string `json:"action,omitempty"`
	Intent              string `json:"intent,omitempty"`
	SelectedSkillID     string `json:"selected_skill_id,omitempty"`
	DecisionReason      string `json:"decision_reason,omitempty"`
	DecisionProfileHash string `json:"decision_profile_hash,omitempty"`
	ExplicitSkillID     string `json:"explicit_skill_id,omitempty"`
}

func (p DecisionBlockedPayload) StreamKind() StreamItemKind { return StreamKindDecisionBlocked }

// --- Skill payloads ---

type SkillDiscoveredPayload struct {
	Skill *StreamSkill `json:"skill,omitempty"`
}

func (p SkillDiscoveredPayload) StreamKind() StreamItemKind { return StreamKindSkillDiscovered }

type SkillSelectedPayload struct {
	Skill *StreamSkill `json:"skill,omitempty"`
}

func (p SkillSelectedPayload) StreamKind() StreamItemKind { return StreamKindSkillSelected }

type SkillLoadedPayload struct {
	Skill *StreamSkill `json:"skill,omitempty"`
}

func (p SkillLoadedPayload) StreamKind() StreamItemKind { return StreamKindSkillLoaded }

type SkillFailedPayload struct {
	Skill *StreamSkill `json:"skill,omitempty"`
}

func (p SkillFailedPayload) StreamKind() StreamItemKind { return StreamKindSkillFailed }

type SkillLifecyclePayload struct {
	SkillLifecycle *StreamSkillLifecycle `json:"skill_lifecycle,omitempty"`
}

func (p SkillLifecyclePayload) StreamKind() StreamItemKind { return StreamKindSkillLifecycle }

type ProcedureActivationPayload struct {
	ProcedureActivation *StreamProcedureActivation `json:"procedure_activation,omitempty"`
}

func (p ProcedureActivationPayload) StreamKind() StreamItemKind {
	return StreamKindProcedureActivation
}

// --- Memory/Context payloads ---

type MemoryPreparedPayload struct {
	MemoryPrepared *StreamMemoryPrepared `json:"memory_prepared,omitempty"`
}

func (p MemoryPreparedPayload) StreamKind() StreamItemKind { return StreamKindMemoryPrepared }

type ContextCompressedPayload struct {
	ContextCompressed *StreamContextCompressed `json:"context_compressed,omitempty"`
}

func (p ContextCompressedPayload) StreamKind() StreamItemKind { return StreamKindContextCompressed }

type ContextPressurePayload struct {
	ContextPressure *StreamContextPressure `json:"context_pressure,omitempty"`
}

func (p ContextPressurePayload) StreamKind() StreamItemKind { return StreamKindContextPressure }

// --- Assistant/Tool payloads ---

type AssistantDeltaPayload struct {
	AssistantDelta *StreamAssistantDelta `json:"assistant_delta,omitempty"`
}

func (p AssistantDeltaPayload) StreamKind() StreamItemKind { return StreamKindAssistantDelta }

type AssistantMessagePayload struct {
	Message *StreamMessage `json:"message,omitempty"`
}

func (p AssistantMessagePayload) StreamKind() StreamItemKind { return StreamKindAssistantMessage }

type StreamAssistantDelta struct {
	Role      string                  `json:"role,omitempty"`
	Delta     string                  `json:"delta,omitempty"`
	Reasoning string                  `json:"reasoning,omitempty"`
	Sequence  int                     `json:"sequence"`
	MessageID string                  `json:"message_id,omitempty"`
	IsFinal   bool                    `json:"is_final,omitempty"`
	ToolCalls []StreamPlannedToolCall `json:"tool_calls,omitempty"`
	Meta      map[string]any          `json:"meta,omitempty"`
}

type ToolCallStartedPayload struct {
	ToolCall *StreamToolCall `json:"tool_call,omitempty"`
}

func (p ToolCallStartedPayload) StreamKind() StreamItemKind { return StreamKindToolCallStarted }

type ToolCallProgressPayload struct {
	ToolCall *StreamToolCallProgress `json:"tool_call,omitempty"`
}

func (p ToolCallProgressPayload) StreamKind() StreamItemKind { return StreamKindToolCallProgress }

type ToolCallSucceededPayload struct {
	ToolCall *StreamToolCall `json:"tool_call,omitempty"`
}

func (p ToolCallSucceededPayload) StreamKind() StreamItemKind { return StreamKindToolCallSucceeded }

type ToolCallFailedPayload struct {
	ToolCall *StreamToolCall `json:"tool_call,omitempty"`
}

func (p ToolCallFailedPayload) StreamKind() StreamItemKind { return StreamKindToolCallFailed }

type ToolCallInterruptedPayload struct {
	ToolCall *StreamToolCall `json:"tool_call,omitempty"`
}

func (p ToolCallInterruptedPayload) StreamKind() StreamItemKind { return StreamKindToolCallInterrupted }

// --- Provider payloads ---

type ProviderDegradedPayload struct {
	AffectedProviders []ProviderDegradedEntry `json:"affected_providers"`
}

func (p ProviderDegradedPayload) StreamKind() StreamItemKind { return StreamKindProviderDegraded }

type ProviderDegradedEntry struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Error     string `json:"error,omitempty"`
}

// --- MCP payloads ---

type MCPProviderLifecyclePayload struct {
	streamKind   StreamItemKind `json:"-"`
	ProviderName string         `json:"provider_name"`
	Transport    string         `json:"transport,omitempty"`
	Error        string         `json:"error,omitempty"`
	AuthStatus   string         `json:"auth_status,omitempty"`
}

func (p MCPProviderLifecyclePayload) StreamKind() StreamItemKind {
	if p.streamKind != "" {
		return p.streamKind
	}
	return StreamKindMCPToolCatalogRefreshed
}

// --- Elicitation payloads ---

type ElicitationPayload struct {
	streamKind      StreamItemKind `json:"-"`
	ActionID        string         `json:"action_id"`
	Message         string         `json:"message"`
	RequestedSchema any            `json:"requested_schema,omitempty"`
}

func (p ElicitationPayload) StreamKind() StreamItemKind {
	if p.streamKind != "" {
		return p.streamKind
	}
	return StreamKindElicitationPending
}

// --- Sampling payloads ---

type SamplingPayload struct {
	streamKind StreamItemKind `json:"-"`
	RunID      string         `json:"run_id"`
	Depth      int32          `json:"depth"`
	Model      string         `json:"model,omitempty"`
}

func (p SamplingPayload) StreamKind() StreamItemKind {
	if p.streamKind != "" {
		return p.streamKind
	}
	return StreamKindSamplingStarted
}

// --- Subagent payloads ---

type SubagentStartedPayload struct {
	SubRunID          string `json:"sub_run_id"`
	ParentID          string `json:"parent_id"`
	SessionID         string `json:"session_id,omitempty"`
	Depth             int    `json:"depth"`
	Task              string `json:"task"`
	ChildRunMode      string `json:"child_run_mode,omitempty"`
	WorkspaceMode     string `json:"workspace_mode,omitempty"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	ContextMessages   int    `json:"context_messages,omitempty"`
	OrchestrationMode string `json:"orchestration_mode,omitempty"`
	ParentStepID      string `json:"parent_step_id,omitempty"`
}

func (p SubagentStartedPayload) StreamKind() StreamItemKind { return StreamKindSubagentStarted }

type SubagentCompletedPayload struct {
	SubRunID          string   `json:"sub_run_id"`
	ParentID          string   `json:"parent_id"`
	SessionID         string   `json:"session_id,omitempty"`
	Summary           string   `json:"summary"`
	FinalStatus       string   `json:"final_status,omitempty"`
	AcceptanceStatus  string   `json:"acceptance_status,omitempty"`
	AcceptanceReasons []string `json:"acceptance_reasons,omitempty"`
	ChildRunMode      string   `json:"child_run_mode,omitempty"`
	WorkspaceMode     string   `json:"workspace_mode,omitempty"`
	WorktreePath      string   `json:"worktree_path,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	OrchestrationMode string   `json:"orchestration_mode,omitempty"`
	ParentStepID      string   `json:"parent_step_id,omitempty"`
}

func (p SubagentCompletedPayload) StreamKind() StreamItemKind { return StreamKindSubagentCompleted }

type SubagentFailedPayload struct {
	SubRunID          string   `json:"sub_run_id"`
	ParentID          string   `json:"parent_id"`
	SessionID         string   `json:"session_id,omitempty"`
	Error             string   `json:"error"`
	AcceptanceStatus  string   `json:"acceptance_status,omitempty"`
	AcceptanceReasons []string `json:"acceptance_reasons,omitempty"`
	ChildRunMode      string   `json:"child_run_mode,omitempty"`
	WorkspaceMode     string   `json:"workspace_mode,omitempty"`
	WorktreePath      string   `json:"worktree_path,omitempty"`
	OrchestrationMode string   `json:"orchestration_mode,omitempty"`
	ParentStepID      string   `json:"parent_step_id,omitempty"`
}

func (p SubagentFailedPayload) StreamKind() StreamItemKind { return StreamKindSubagentFailed }

type HeartbeatPayload struct {
	Timestamp int64 `json:"timestamp"`
}

func (p HeartbeatPayload) StreamKind() StreamItemKind { return StreamKindHeartbeat }

// --- Tool parallel batch payloads ---

type ToolParallelBatchStartedPayload struct {
	ToolNames []string `json:"tool_names"`
}

func (p ToolParallelBatchStartedPayload) StreamKind() StreamItemKind {
	return StreamKindToolParallelBatchStarted
}

type ToolParallelBatchCompletedPayload struct {
	ToolNames []string `json:"tool_names"`
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
}

func (p ToolParallelBatchCompletedPayload) StreamKind() StreamItemKind {
	return StreamKindToolParallelBatchCompleted
}

// --- Shared value types ---

type StreamPlannedToolCall struct {
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	ArgumentsJSON string `json:"arguments_json,omitempty"`
}

type StreamMessage struct {
	Role       string                  `json:"role,omitempty"`
	Content    string                  `json:"content,omitempty"`
	Reasoning  string                  `json:"reasoning,omitempty"`
	ToolCalls  []StreamPlannedToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	ToolName   string                  `json:"tool_name,omitempty"`
	Meta       map[string]any          `json:"meta,omitempty"`
}

type StreamToolCall struct {
	Provider          string `json:"provider,omitempty"`
	Name              string `json:"name,omitempty"`
	CallID            string `json:"call_id,omitempty"`
	ArgumentsJSON     string `json:"arguments_json,omitempty"`
	InterruptID       string `json:"interrupt_id,omitempty"`
	Output            string `json:"output,omitempty"`
	Error             string `json:"error,omitempty"`
	DurationMS        int64  `json:"duration_ms,omitempty"`
	InterruptContexts int    `json:"interrupt_contexts,omitempty"`
}

type StreamToolCallProgress struct {
	Provider      string `json:"provider,omitempty"`
	Name          string `json:"name,omitempty"`
	CallID        string `json:"call_id,omitempty"`
	ArgumentsJSON string `json:"arguments_json,omitempty"`
	Delta         string `json:"delta,omitempty"`
	Sequence      int    `json:"sequence"`
}

type StreamInterruptContext struct {
	ID          string `json:"id,omitempty"`
	Address     string `json:"address,omitempty"`
	Info        any    `json:"info,omitempty"`
	IsRootCause bool   `json:"is_root_cause,omitempty"`
}

type StreamInterrupt struct {
	ContextCount int                      `json:"context_count,omitempty"`
	Contexts     []StreamInterruptContext `json:"contexts,omitempty"`
}

type StreamSkillCandidate struct {
	ID             string                  `json:"id,omitempty"`
	Name           string                  `json:"name,omitempty"`
	Score          int                     `json:"score,omitempty"`
	MatchedTerms   []string                `json:"matched_terms,omitempty"`
	FilteredReason string                  `json:"filtered_reason,omitempty"`
	Requirements   StreamSkillRequirements `json:"requirements,omitempty"`
	Summary        string                  `json:"summary,omitempty"`
	Origin         string                  `json:"origin,omitempty"`
	TaskPattern    string                  `json:"task_pattern,omitempty"`
}

type StreamSkill struct {
	SelectedID        string                  `json:"selected_id,omitempty"`
	Name              string                  `json:"name,omitempty"`
	Source            string                  `json:"source,omitempty"`
	Origin            string                  `json:"origin,omitempty"`
	TaskPattern       string                  `json:"task_pattern,omitempty"`
	Path              string                  `json:"path,omitempty"`
	Candidates        []StreamSkillCandidate  `json:"candidates,omitempty"`
	NoSelectionReason string                  `json:"no_selection_reason,omitempty"`
	Summary           string                  `json:"summary,omitempty"`
	Instruction       string                  `json:"instruction,omitempty"`
	Scripts           []string                `json:"scripts,omitempty"`
	Requirements      StreamSkillRequirements `json:"requirements,omitempty"`
	Score             int                     `json:"score,omitempty"`
	MatchedTerms      []string                `json:"matched_terms,omitempty"`
	RunStatus         string                  `json:"run_status,omitempty"`
	PromotedFrom      string                  `json:"promoted_from,omitempty"`
	FailureReason     string                  `json:"failure_reason,omitempty"`
}

type StreamSkillLifecycle struct {
	SkillID         string         `json:"skill_id"`
	Action          string         `json:"action"`
	Status          string         `json:"status,omitempty"`
	Verdict         string         `json:"verdict,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	EvidenceRefs    []string       `json:"evidence_refs,omitempty"`
	AssessmentID    string         `json:"assessment_id,omitempty"`
	ChangesRequired []string       `json:"changes_required,omitempty"`
	Applied         bool           `json:"applied,omitempty"`
	Assessment      map[string]any `json:"assessment,omitempty"`
}

type StreamSkillRequirements struct {
	Tools    []string `json:"tools,omitempty"`
	Toolsets []string `json:"toolsets,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Env      []string `json:"env,omitempty"`
}

type StreamProcedureActivation struct {
	RunID        string   `json:"run_id,omitempty"`
	SessionID    string   `json:"session_id,omitempty"`
	ProcedureRef string   `json:"procedure_ref,omitempty"`
	Title        string   `json:"title,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Phase        string   `json:"phase,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Score        float64  `json:"score,omitempty"`
	Status       string   `json:"status,omitempty"`
	Origin       string   `json:"origin,omitempty"`
	SourceRefs   []string `json:"source_refs,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type StreamMemoryPreparedNudge struct {
	Ref    string `json:"ref,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type StreamMemoryPreparedEntry struct {
	Ref   string `json:"ref,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Title string `json:"title,omitempty"`
}

type StreamMemoryPrepared struct {
	Query          string                      `json:"query,omitempty"`
	WorkspaceScope string                      `json:"workspace_scope,omitempty"`
	NudgeCount     int                         `json:"nudge_count,omitempty"`
	EntryCount     int                         `json:"entry_count,omitempty"`
	Nudges         []StreamMemoryPreparedNudge `json:"nudges,omitempty"`
	Entries        []StreamMemoryPreparedEntry `json:"entries,omitempty"`
}

type StreamContextCompressed struct {
	BoundaryID     string `json:"boundary_id,omitempty"`
	FirstIndex     int    `json:"first_index,omitempty"`
	LastIndex      int    `json:"last_index,omitempty"`
	TokensBefore   int    `json:"tokens_before,omitempty"`
	TokensAfter    int    `json:"tokens_after,omitempty"`
	SummarySnippet string `json:"summary_snippet,omitempty"`
}

type StreamContextPressure struct {
	State                      string `json:"state,omitempty"`
	EstimatedInputTokens       int    `json:"estimated_input_tokens,omitempty"`
	EffectiveWindowTokens      int    `json:"effective_window_tokens,omitempty"`
	WarningThresholdTokens     int    `json:"warning_threshold_tokens,omitempty"`
	AutoCompactThresholdTokens int    `json:"auto_compact_threshold_tokens,omitempty"`
	BlockingThresholdTokens    int    `json:"blocking_threshold_tokens,omitempty"`
	PercentUsed                int    `json:"percent_used,omitempty"`
}

// --- Plan payloads ---

// StreamPlan is the plan state carried in plan stream events.
type StreamPlan struct {
	PlanID    string         `json:"plan_id"`
	SessionID string         `json:"session_id"`
	RunID     string         `json:"run_id"`
	Steps     []api.PlanStep `json:"steps"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type PlanCreatedPayload struct {
	Plan *StreamPlan `json:"plan"`
}

func (p *PlanCreatedPayload) StreamKind() StreamItemKind { return StreamKindPlanCreated }

type PlanUpdatedPayload struct {
	Plan *StreamPlan `json:"plan"`
}

func (p *PlanUpdatedPayload) StreamKind() StreamItemKind { return StreamKindPlanUpdated }

type PlanClearedPayload struct {
	PlanID string `json:"plan_id"`
}

func (p *PlanClearedPayload) StreamKind() StreamItemKind { return StreamKindPlanCleared }

type CrystallizationFailedPayload struct {
	RunID string `json:"run_id"`
	Error string `json:"error"`
}

func (p *CrystallizationFailedPayload) StreamKind() StreamItemKind {
	return StreamKindCrystallizationFailed
}

type CrystallizationVerdictPayload struct {
	RunID     string `json:"run_id"`
	Verdict   string `json:"verdict"`
	SkillID   string `json:"skill_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
	SimilarTo string `json:"similar_to,omitempty"`
}

func (p *CrystallizationVerdictPayload) StreamKind() StreamItemKind {
	return StreamKindCrystallizationVerdict
}

type PlanStepPayload struct {
	PlanID    string        `json:"plan_id"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Plan      *StreamPlan   `json:"plan"`
	Step      *api.PlanStep `json:"step"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type PlanStepStartedPayload struct {
	PlanStepPayload
}

func (p *PlanStepStartedPayload) StreamKind() StreamItemKind { return StreamKindStepStarted }

type PlanStepCompletedPayload struct {
	PlanStepPayload
}

func (p *PlanStepCompletedPayload) StreamKind() StreamItemKind { return StreamKindStepCompleted }

type PlanStepFailedPayload struct {
	PlanStepPayload
	Error string `json:"error,omitempty"`
}

func (p *PlanStepFailedPayload) StreamKind() StreamItemKind { return StreamKindStepFailed }
