package runtime

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/stream"
)

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink stream.StreamSink, kind stream.StreamItemKind, payload map[string]any) error {
	_, err := stream.AppendStreamItem(ctx, e.store, sink, stream.StreamItem{
		RunID:     runID,
		Kind:      kind,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink stream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, stream.StreamKindRunStarted, map[string]any{"input": input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink stream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, stream.StreamKindRunResumeRequested, map[string]any{"targets": targets})
}

func (e *Executor) emitRunCompleted(ctx context.Context, runID, output string, sink stream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, stream.StreamKindRunCompleted, map[string]any{"message": &stream.StreamMessage{
		Role:    string(schema.Assistant),
		Content: output,
	}})
}

func (e *Executor) emitRunFailed(ctx context.Context, runID string, sink stream.StreamSink, message string) error {
	return e.emitLifecyclePayload(ctx, runID, sink, stream.StreamKindRunFailed, map[string]any{"error": message})
}
