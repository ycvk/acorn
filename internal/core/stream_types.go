package core

import (
	"encoding/json"
	"errors"
	"fmt"
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
	StreamKindMemoryPrepared     StreamItemKind = "memory_prepared"
	StreamKindAssistantDelta     StreamItemKind = "assistant.delta"
	StreamKindAssistantMessage   StreamItemKind = "assistant_message"
	StreamKindToolCallSucceeded  StreamItemKind = "tool_call_succeeded"
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

// MarshalJSON serializes StreamItem with payload fields flattened into the
// top-level object. The "kind" field acts as the discriminator.
func (item StreamItem) MarshalJSON() ([]byte, error) {
	obj := map[string]any{
		"run_id":     item.RunID,
		"kind":       string(item.Kind),
		"created_at": item.CreatedAt,
	}
	if item.Sequence != 0 {
		obj["sequence"] = item.Sequence
	}
	for k, v := range item.Payload {
		obj[k] = v
	}
	return json.Marshal(obj)
}

// UnmarshalJSON deserializes flat StreamItem JSON, extracting common fields
// and keeping the remaining keys as the payload map.
func (item *StreamItem) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	runID, ok := raw["run_id"].(string)
	if !ok {
		return errors.New("stream item run_id must be a string")
	}
	kindStr, ok := raw["kind"].(string)
	if !ok {
		return errors.New("stream item kind must be a string")
	}

	var sequence int64
	if seq, ok := raw["sequence"]; ok {
		switch v := seq.(type) {
		case float64:
			sequence = int64(v)
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return fmt.Errorf("parse stream item sequence: %w", err)
			}
			sequence = n
		}
	}

	var createdAt time.Time
	if ca, ok := raw["created_at"]; ok {
		if caStr, ok := ca.(string); ok {
			t, err := time.Parse(time.RFC3339Nano, caStr)
			if err != nil {
				t, err = time.Parse(time.RFC3339, caStr)
				if err != nil {
					return fmt.Errorf("parse created_at: %w", err)
				}
			}
			createdAt = t
		}
	}

	item.RunID = runID
	item.Kind = StreamItemKind(kindStr)
	item.Sequence = sequence
	item.CreatedAt = createdAt

	delete(raw, "run_id")
	delete(raw, "kind")
	delete(raw, "sequence")
	delete(raw, "created_at")
	item.Payload = raw

	return nil
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
