package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/eino/schema"
)

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink StreamSink, payload StreamPayload) error {
	if e == nil || e.store == nil {
		return errors.New("executor store is nil")
	}
	if payload == nil {
		return errors.New("lifecycle payload is nil")
	}
	_, err := AppendStreamItem(ctx, e.store, sink, StreamItem{
		RunID:     runID,
		Kind:      payload.StreamKind(),
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &RunStartedPayload{Input: input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &RunResumeRequestedPayload{Targets: targets})
}

func (e *Executor) emitRunCompleted(ctx context.Context, runID, output string, sink StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &RunCompletedPayload{
		Message: &StreamMessage{
			Role:    string(schema.Assistant),
			Content: output,
		},
	})
}

func (e *Executor) emitRunFailed(ctx context.Context, runID string, sink StreamSink, message string) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &RunFailedPayload{Error: message})
}
