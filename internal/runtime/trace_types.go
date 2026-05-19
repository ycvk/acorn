package runtime

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

type streamSinkContextKey struct{}

func withStreamSink(ctx context.Context, sink StreamSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, streamSinkContextKey{}, sink)
}

func streamSinkFromContext(ctx context.Context) StreamSink {
	if ctx == nil {
		return nil
	}
	sink, ok := ctx.Value(streamSinkContextKey{}).(StreamSink)
	if !ok {
		return nil
	}
	return sink
}
