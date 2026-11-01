package core

import (
	"time"
)

// --- Stream item kinds ---

type StreamItemKind string

const (
	StreamKindRunStarted         StreamItemKind = "run_started"
	StreamKindRunCompleted       StreamItemKind = "run_completed"
	StreamKindRunFailed          StreamItemKind = "run_failed"
	StreamKindRunInterrupted     StreamItemKind = "run_interrupted"
	StreamKindRunResumeRequested StreamItemKind = "run_resume_requested"
	StreamKindAssistantDelta     StreamItemKind = "assistant.delta"
	StreamKindAssistantMessage   StreamItemKind = "assistant_message"
	StreamKindToolCallStarted    StreamItemKind = "tool_call_started"
	StreamKindToolCallSucceeded  StreamItemKind = "tool_call_succeeded"
	StreamKindToolCallFailed     StreamItemKind = "tool_call_failed"
	StreamKindElicitationPending StreamItemKind = "elicitation.pending"
	StreamKindElicitationDecided StreamItemKind = "elicitation.decided"
)

// StreamItem is a single event in the run runtime.
type StreamItem struct {
	RunID     string         `json:"run_id"`
	Sequence  int64          `json:"sequence,omitempty"`
	Kind      StreamItemKind `json:"kind"`
	CreatedAt time.Time      `json:"created_at"`
	Payload   map[string]any `json:"-"`
}

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
