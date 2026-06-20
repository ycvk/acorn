package runtime

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/stream"
)

type RunState struct {
	lastOutput       string
	interrupt        map[string]any
	failure          error
	emittedRunFailed bool
}

func (e *Executor) consume(ctx context.Context, runID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], selectedSkill *SelectedSkill, sink stream.StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	return e.finishCollectedRun(ctx, runID, input, state, selectedSkill, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink stream.StreamSink, chatModel einomodel.BaseChatModel) (RunState, error) {
	state := RunState{}
	for {
		event, ok := iter.Next()
		if !ok {
			return state, nil
		}
		if err := e.applyAgentEvent(ctx, runID, stream.StreamItemsFromAgentEvent(event, chatModel), sink, &state); err != nil {
			return RunState{}, err
		}
	}
}

func (e *Executor) applyAgentEvent(ctx context.Context, runID string, items []stream.StreamItem, sink stream.StreamSink, state *RunState) error {
	for _, item := range items {
		item.RunID = runID
		if _, err := stream.AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
	}
	return nil
}

func (s *RunState) applyStreamItem(item stream.StreamItem) {
	if delta := item.GetAssistantDelta(); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := item.GetMessage(); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
	if interrupt := item.GetInterrupt(); interrupt != nil {
		s.interrupt = InterruptPayloadFromStream(interrupt)
	}
	if item.Kind == stream.StreamKindRunFailed && item.GetError() != "" {
		s.failure = errors.New(item.GetError())
		s.emittedRunFailed = true
	}
}
