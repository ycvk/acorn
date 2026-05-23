package runstream

import (
	"context"

	"github.com/ycvk/acorn/internal/events"
)

// --- Trace types ---

type Trace struct {
	Run     *events.RunRecord `json:"run,omitempty"`
	Summary *TraceSummary     `json:"summary,omitempty"`
	Items   []StreamItem      `json:"items,omitempty"`
}

type TraceSummary struct {
	ItemCount                  int            `json:"item_count"`
	LastKind                   StreamItemKind `json:"last_kind,omitempty"`
	AssistantMessageCount      int            `json:"assistant_message_count,omitempty"`
	AssistantDeltaCount        int            `json:"assistant_delta_count,omitempty"`
	AssistantDeltaMessageCount int            `json:"assistant_delta_message_count,omitempty"`
	AssistantDeltaCharCount    int            `json:"assistant_delta_char_count,omitempty"`
	ToolCallCount              int            `json:"tool_call_count,omitempty"`
	DecisionEventCount         int            `json:"decision_event_count,omitempty"`
	SkillEventCount            int            `json:"skill_event_count,omitempty"`
	PlanEventCount             int            `json:"plan_event_count,omitempty"`
	DecisionSelected           bool           `json:"decision_selected,omitempty"`
	DecisionBlocked            bool           `json:"decision_blocked,omitempty"`
	SkillSelected              bool           `json:"skill_selected,omitempty"`
	Interrupted                bool           `json:"interrupted,omitempty"`
	Failed                     bool           `json:"failed,omitempty"`
	Completed                  bool           `json:"completed,omitempty"`
}

// --- StreamSink ---

type StreamSink func(item StreamItem) error

type streamSinkContextKey struct{}

func WithStreamSink(ctx context.Context, sink StreamSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, streamSinkContextKey{}, sink)
}

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

// --- Result ---

type Result struct {
	RunID        string           `json:"run_id"`
	Status       events.RunStatus `json:"status"`
	Output       string           `json:"output,omitempty"`
	Error        string           `json:"error,omitempty"`
	Interrupted  map[string]any   `json:"interrupted,omitempty"`
	TraceSummary *TraceSummary    `json:"trace_summary,omitempty"`
}

// --- Errors ---

var (
	ErrRunNotActive      = NewRunError("run not active")
	ErrRunNotInterrupted = NewRunError("run not interrupted")
	ErrExecutionNotReady = NewRunError("execution not ready")
)

type runError string

func NewRunError(msg string) error {
	return runError(msg)
}

func (e runError) Error() string {
	return string(e)
}

// --- SessionState ---

type SessionState string

const (
	SessionStateNew         SessionState = "new"
	SessionStateRunning     SessionState = "running"
	SessionStateCompleted   SessionState = "completed"
	SessionStateFailed      SessionState = "failed"
	SessionStateInterrupted SessionState = "interrupted"
	SessionStateDegraded    SessionState = "degraded"
)

func DeriveSessionState(latestRun *events.RunRecord, hasDegradedProvider bool) SessionState {
	if latestRun == nil {
		return SessionStateNew
	}
	switch latestRun.Status {
	case events.RunStatusSucceeded:
		if hasDegradedProvider {
			return SessionStateDegraded
		}
		return SessionStateCompleted
	case events.RunStatusFailed:
		return SessionStateFailed
	case events.RunStatusRunning:
		return SessionStateRunning
	case events.RunStatusInterrupted:
		if hasDegradedProvider {
			return SessionStateDegraded
		}
		return SessionStateInterrupted
	default:
		return SessionStateDegraded
	}
}
