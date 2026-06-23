package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
)

// --- Sentinel errors ---

var (
	ErrRunNotActive      = errors.New("run not active")
	ErrRunNotInterrupted = errors.New("run not interrupted")
	ErrExecutionNotReady = errors.New("execution not ready")
)

type RunStatus string

const (
	RunStatusRunning     RunStatus = "running"
	RunStatusSucceeded   RunStatus = "succeeded"
	RunStatusInterrupted RunStatus = "interrupted"
	RunStatusFailed      RunStatus = "failed"
)

// --- Run / event / session records ---

type RunRecord struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id,omitempty"`
	TurnIndex int       `json:"turn_index,omitempty"`
	Status    RunStatus `json:"status"`
	Input     string    `json:"input"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EventRecord struct {
	Sequence  int64     `json:"sequence"`
	RunID     string    `json:"run_id"`
	Kind      string    `json:"kind"`
	Payload   any       `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionRecord struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EventKind string

// --- Pending actions ---

type PendingActionKind string

const (
	PendingActionKindElicitation      PendingActionKind = "elicitation"
	PendingActionKindOperatorQuestion PendingActionKind = "operator_question"
)

type PendingActionStatus string

const (
	PendingActionStatusPending  PendingActionStatus = "pending"
	PendingActionStatusApproved PendingActionStatus = "approved"
	PendingActionStatusRejected PendingActionStatus = "rejected"
	PendingActionStatusResolved PendingActionStatus = "resolved"
)

type PendingActionRecord struct {
	ActionID     string              `json:"action_id"`
	RunID        string              `json:"run_id"`
	InterruptID  string              `json:"interrupt_id,omitempty"`
	Kind         PendingActionKind   `json:"kind"`
	Subject      string              `json:"subject,omitempty"`
	PayloadJSON  string              `json:"payload_json"`
	Status       PendingActionStatus `json:"status"`
	Reason       string              `json:"reason,omitempty"`
	DecisionJSON string              `json:"decision_json,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	DecidedAt    *time.Time          `json:"decided_at,omitempty"`
	ResolvedAt   *time.Time          `json:"resolved_at,omitempty"`
}

// --- Operator question ---

const (
	OperatorQuestionDecisionAnswer  = "answer"
	OperatorQuestionDecisionDecline = "decline"
)

type PendingActionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type OperatorQuestionPayload struct {
	Question      string                `json:"question"`
	Options       []PendingActionOption `json:"options,omitempty"`
	AllowFreeform bool                  `json:"allow_freeform,omitempty"`
}

type OperatorQuestionDecision struct {
	Action           string `json:"action"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

// --- Session messages ---

type SessionMessageRecord struct {
	ID           int64           `json:"id"`
	SessionID    string          `json:"session_id"`
	TurnIndex    int             `json:"turn_index"`
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	ContentParts json.RawMessage `json:"content_parts,omitempty"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// --- Session summary ---

type SessionSummary struct {
	SessionID   string    `json:"session_id"`
	SourceRunID string    `json:"source_run_id"`
	RunStatus   string    `json:"run_status"`
	Summary     string    `json:"summary"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// --- Ports ---

// EventAppender appends a runtime event to the persisted store.
type EventAppender interface {
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (EventRecord, error)
}

// RunContextBridge provides access to the current run and session identifiers.
type RunContextBridge interface {
	CurrentRunID(ctx context.Context) string
	CurrentSessionID(ctx context.Context) string
}

// ToolCallContextBridge extends RunContextBridge with tool-call-scoped identity.
type ToolCallContextBridge interface {
	RunContextBridge
	CurrentToolCallID(ctx context.Context) string
}

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

// --- Context plumbing ---

type runIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func GetRunID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(runIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}

type sessionIDContextKey struct{}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(sessionIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}

type turnIndexContextKey struct{}

func WithTurnIndex(ctx context.Context, turnIndex int) context.Context {
	return context.WithValue(ctx, turnIndexContextKey{}, turnIndex)
}

func TurnIndexFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	index, ok := ctx.Value(turnIndexContextKey{}).(int)
	if !ok {
		return 0
	}
	return index
}

// --- Call-site context plumbing ---

const (
	CallSiteRuntime    = "runtime"
	CallSiteAssistant  = "assistant"
	CallSiteCompaction = "compaction"
)

type callSiteContextKey struct{}

func WithCallSite(ctx context.Context, callSite string) context.Context {
	trimmed := strings.TrimSpace(callSite)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, callSiteContextKey{}, trimmed)
}

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

type streamSinkContextKey struct{}

// WithStreamSink attaches a StreamSink to the context for retrieval by StreamSinkFromContext.
func WithStreamSink(ctx context.Context, sink StreamSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, streamSinkContextKey{}, sink)
}

// StreamSinkFromContext retrieves the StreamSink previously attached via WithStreamSink.
func StreamSinkFromContext(ctx context.Context) StreamSink {
	if ctx == nil {
		return nil
	}
	sink, ok := ctx.Value(streamSinkContextKey{}).(StreamSink)
	if !ok {
		return nil
	}
	return sink
}

// --- JSON serializer (eino compose) ---

type JSONSerializer struct{}

var _ compose.Serializer = (*JSONSerializer)(nil)

func (j *JSONSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (j *JSONSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
