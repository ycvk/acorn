package stream

import (
	"time"

	"github.com/ycvk/acorn/internal/model"
)

// Shared value types used across stream payloads.

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

// PlanStepPayload is the common base for plan step events.
type PlanStepPayload struct {
	PlanID    string          `json:"plan_id"`
	SessionID string          `json:"session_id"`
	RunID     string          `json:"run_id"`
	Plan      *model.Plan     `json:"plan"`
	Step      *model.PlanStep `json:"step"`
	UpdatedAt time.Time       `json:"updated_at"`
}
