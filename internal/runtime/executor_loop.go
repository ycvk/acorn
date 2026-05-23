package runtime

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
)

type runState struct {
	lastOutput       string
	interrupt        map[string]any
	failure          error
	emittedRunFailed bool
}

func (e *Executor) consume(ctx context.Context, runID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], selectedSkill *SelectedSkill, sink StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	if err := e.runBuilder.ConsumeEventError(runID); err != nil {
		state.failure = err
	}
	if rc, ok := e.runBuilder.Registry().Get(runID); ok {
		rc.SetFinalizing()
	}
	return e.finishCollectedRun(ctx, runID, input, state, selectedSkill, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink StreamSink, chatModel einomodel.BaseChatModel) (runState, error) {
	state := runState{}
	for {
		event, ok := iter.Next()
		if !ok {
			return state, nil
		}
		if err := e.applyAgentEvent(ctx, runID, StreamItemsFromAgentEvent(event, chatModel), sink, &state); err != nil {
			return runState{}, err
		}
	}
}

func (e *Executor) prepareSkillExecution(ctx context.Context, runID string, selected *SelectedSkill, downstreamSink StreamSink) (StreamSink, error) {
	_ = ctx
	_ = runID
	_ = selected
	return downstreamSink, nil
}

func (e *Executor) applyAgentEvent(ctx context.Context, runID string, items []StreamItem, sink StreamSink, state *runState) error {
	for _, item := range items {
		item.RunID = runID
		if _, err := AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
	}
	return nil
}

func (s *runState) applyStreamItem(item StreamItem) {
	if delta := item.GetAssistantDelta(); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := item.GetMessage(); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
	if interrupt := item.GetInterrupt(); interrupt != nil {
		s.interrupt = InterruptPayloadFromStream(interrupt)
	}
	if item.Kind == StreamKindRunFailed && item.GetError() != "" {
		s.failure = errors.New(item.GetError())
		s.emittedRunFailed = true
	}
}
