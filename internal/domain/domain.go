package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/core"
)

// --- Sentinel errors ---

var (
	ErrRunNotActive      = errors.New("run not active")
	ErrRunNotInterrupted = errors.New("run not interrupted")
	ErrExecutionNotReady = errors.New("execution not ready")
)

// The types below are aliases for their canonical definitions in internal/core
// (see store_types.go for the rationale). The legacy const blocks are retained
// so existing domain.* references keep compiling; each const is now typed
// against the aliased (core) type.

type RunStatus = core.RunStatus

const (
	RunStatusRunning     = core.RunStatusRunning
	RunStatusSucceeded   = core.RunStatusSucceeded
	RunStatusInterrupted = core.RunStatusInterrupted
	RunStatusFailed      = core.RunStatusFailed
)

// --- Run / event / session records ---

type RunRecord = core.RunRecord
type EventRecord = core.EventRecord
type SessionRecord = core.SessionRecord
type EventKind = core.EventKind

// --- Pending actions ---

type PendingActionKind = core.PendingActionKind

const (
	PendingActionKindElicitation      = core.PendingActionKindElicitation
	PendingActionKindOperatorQuestion = core.PendingActionKindOperatorQuestion
)

type PendingActionStatus = core.PendingActionStatus

const (
	PendingActionStatusPending  = core.PendingActionStatusPending
	PendingActionStatusApproved = core.PendingActionStatusApproved
	PendingActionStatusRejected = core.PendingActionStatusRejected
	PendingActionStatusResolved = core.PendingActionStatusResolved
)

type PendingActionRecord = core.PendingActionRecord

// --- Operator question ---

const (
	OperatorQuestionDecisionAnswer  = core.OperatorQuestionDecisionAnswer
	OperatorQuestionDecisionDecline = core.OperatorQuestionDecisionDecline
)

type PendingActionOption = core.PendingActionOption
type OperatorQuestionPayload = core.OperatorQuestionPayload
type OperatorQuestionDecision = core.OperatorQuestionDecision

// --- Session messages ---

type SessionMessageRecord = core.SessionMessageRecord

// --- Session summary ---

type SessionSummary = core.SessionSummary

// --- ExecuteRequest ---

type ExecuteRequest struct {
	RunID            string
	SessionID        string
	TurnIndex        int
	Input            string
	BoundMessageID   int64
	SkillID          string
	AllowedToolNames []string
	Messages         []adk.Message
}

// --- Stream items ---

type StreamItemKind string

const (
	StreamKindRunStarted          StreamItemKind = "run_started"
	StreamKindRunCompleted        StreamItemKind = "run_completed"
	StreamKindRunFailed           StreamItemKind = "run_failed"
	StreamKindRunInterrupted      StreamItemKind = "run_interrupted"
	StreamKindRunResumeRequested  StreamItemKind = "run_resume_requested"
	StreamKindDecisionBlocked     StreamItemKind = "decision_blocked"
	StreamKindSkillDiscovered     StreamItemKind = "skill_discovered"
	StreamKindSkillSelected       StreamItemKind = "skill_selected"
	StreamKindSkillLoaded         StreamItemKind = "skill_loaded"
	StreamKindSkillFailed         StreamItemKind = "skill_failed"
	StreamKindProcedureActivation StreamItemKind = "procedure.activation"
	StreamKindMemoryPrepared      StreamItemKind = "memory_prepared"
	StreamKindAssistantDelta      StreamItemKind = "assistant.delta"
	StreamKindAssistantMessage    StreamItemKind = "assistant_message"
	StreamKindToolCallStarted     StreamItemKind = "tool_call_started"
	StreamKindToolCallSucceeded   StreamItemKind = "tool_call_succeeded"
	StreamKindToolCallFailed      StreamItemKind = "tool_call_failed"
	StreamKindToolCallInterrupted StreamItemKind = "tool_call_interrupted"
	StreamKindElicitationPending  StreamItemKind = "elicitation.pending"
	StreamKindElicitationDecided  StreamItemKind = "elicitation.decided"
	StreamKindSubagentStarted     StreamItemKind = "subagent.started"
	StreamKindSubagentCompleted   StreamItemKind = "subagent.completed"
	StreamKindSubagentFailed      StreamItemKind = "subagent.failed"
)

// StreamItem is a single event in the run stream.
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

// StreamSink consumes run stream items.
type StreamSink func(item StreamItem) error
