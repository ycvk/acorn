package domain

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// --- Stream payload value types ---

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

type StreamSkillRequirements struct {
	Tools    []string `json:"tools,omitempty"`
	Toolsets []string `json:"toolsets,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Env      []string `json:"env,omitempty"`
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

// --- Assistant stream types ---

type AssistantStreamRequest struct {
	RunID     string
	MessageID string
	Model     einomodel.BaseChatModel
	Messages  []*schema.Message
	ToolInfos []*schema.ToolInfo
	CallSite  string
}

type AssistantStopReason string

const (
	AssistantStopReasonEndTurn   AssistantStopReason = "end_turn"
	AssistantStopReasonToolCalls AssistantStopReason = "tool_calls"
	AssistantStopReasonMaxOutput AssistantStopReason = "max_output"
	AssistantStopReasonUnknown   AssistantStopReason = "unknown"
)

type AssistantStreamResult struct {
	Message    *schema.Message
	StopReason AssistantStopReason
	RawReason  string
}

type InterleavedStream struct {
	ToolCallCh     chan schema.ToolCall
	FinalMessageCh chan AssistantStreamResult
	ErrCh          chan error
}

// AssistantStreamer streams assistant messages and interleaved tool calls.
type AssistantStreamer interface {
	StreamAssistantMessage(ctx context.Context, req AssistantStreamRequest) (*AssistantStreamResult, error)
	StreamAssistantInterleaved(ctx context.Context, req AssistantStreamRequest) *InterleavedStream
}
