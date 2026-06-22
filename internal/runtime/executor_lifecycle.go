package runtime

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/runtime/eventstream"
)

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink eventstream.StreamSink, kind eventstream.StreamItemKind, payload map[string]any) error {
	_, err := eventstream.AppendStreamItem(ctx, e.store, sink, eventstream.StreamItem{
		RunID:     runID,
		Kind:      kind,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink eventstream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, eventstream.StreamKindRunStarted, map[string]any{"input": input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink eventstream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, eventstream.StreamKindRunResumeRequested, map[string]any{"targets": targets})
}

func (e *Executor) emitRunCompleted(ctx context.Context, runID, output string, sink eventstream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, eventstream.StreamKindRunCompleted, map[string]any{"message": &eventstream.StreamMessage{
		Role:    string(schema.Assistant),
		Content: output,
	}})
}

func (e *Executor) emitRunFailed(ctx context.Context, runID string, sink eventstream.StreamSink, message string) error {
	return e.emitLifecyclePayload(ctx, runID, sink, eventstream.StreamKindRunFailed, map[string]any{"error": message})
}
