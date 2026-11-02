package runtime

import (
	"context"
	"slices"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// queuedMessageTaker moves the owner messages queued in a conversation into
// its running run.
type queuedMessageTaker interface {
	TakeQueuedMessages(ctx context.Context, runID string) ([]core.SessionMessageRecord, error)
}

// interjectionMiddleware hands the model what the owner sent while the run
// was going. Before each model call it takes the conversation's queued
// messages into the run and appends them to the agent's state, so they stay
// in the run's history from then on. It follows memory visibility, which
// rebuilds the state from the run's history, so the taken messages join that
// history too.
type interjectionMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	queue  queuedMessageTaker
	runID  string
	memory *runMemory
}

func newInterjectionMiddleware(queue queuedMessageTaker, runID string, memory *runMemory) *interjectionMiddleware {
	return &interjectionMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		queue:                             queue,
		runID:                             runID,
		memory:                            memory,
	}
}

func (m *interjectionMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], _ *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	taken, err := m.queue.TakeQueuedMessages(ctx, m.runID)
	if err != nil {
		return ctx, nil, err
	}
	if len(taken) == 0 {
		return ctx, state, nil
	}
	clone := *state
	clone.Messages = slices.Clone(state.Messages)
	for _, message := range taken {
		msg := schema.UserAgenticMessage(message.Content)
		msg.Extra = map[string]any{"memory_source_id": core.MessageSourceID(message.ID)}
		clone.Messages = append(clone.Messages, msg)
		m.memory.history.Messages = append(m.memory.history.Messages, message)
		m.memory.history.SourceIDs = append(m.memory.history.SourceIDs, core.MessageSourceID(message.ID))
	}
	return ctx, &clone, nil
}
