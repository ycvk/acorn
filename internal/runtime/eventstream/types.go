package eventstream

import (
	"context"
)

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

func compactInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
