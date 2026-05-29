package contextplane

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type toolLifecycleMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func NewToolLifecycleMiddleware() adk.ChatModelAgentMiddleware {
	return &toolLifecycleMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
	}
}

func (m *toolLifecycleMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	_ *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	cloned := &adk.ChatModelAgentState{
		Messages: make([]adk.Message, len(state.Messages)),
	}
	copy(cloned.Messages, state.Messages)
	currentTurn := countConversationTurns(cloned.Messages)
	cloned.Messages = PruneToolMessages(ctx, cloned.Messages, currentTurn)
	return ctx, cloned, nil
}

func countConversationTurns(messages []adk.Message) int {
	turns := 0
	for _, msg := range messages {
		if msg != nil && msg.Role == schema.User {
			turns++
		}
	}
	return turns
}
