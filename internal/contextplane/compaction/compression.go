package compaction

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
)

type CompressionMiddlewareBuilder struct{}

func NewCompressionMiddlewareBuilder() *CompressionMiddlewareBuilder {
	return &CompressionMiddlewareBuilder{}
}

func (*CompressionMiddlewareBuilder) Build(
	ctx context.Context,
	cfg config.ContextConfig,
	chatModel einomodel.BaseChatModel,
	opts contextplane.CompressionBuildOptions,
) ([]adk.ChatModelAgentMiddleware, error) {
	_ = cfg
	_ = chatModel
	_ = opts

	patchMW, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("build patchtoolcalls middleware: %w", err)
	}
	lifecycleMW := newToolLifecycleMiddleware()
	return []adk.ChatModelAgentMiddleware{patchMW, lifecycleMW}, nil
}

type toolLifecycleMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newToolLifecycleMiddleware() adk.ChatModelAgentMiddleware {
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
	cloned.Messages = contextplane.PruneToolMessages(ctx, cloned.Messages, currentTurn)
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
