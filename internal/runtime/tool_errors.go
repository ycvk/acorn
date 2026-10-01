package runtime

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

// toolErrorMiddleware turns ordinary tool failures into tool results the model
// can read and react to. Interrupts and run cancellation still propagate, so
// approvals pause the run and a cancelled run stops.
type toolErrorMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newToolErrorMiddleware() *toolErrorMiddleware {
	return &toolErrorMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *toolErrorMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
		output, err := endpoint(ctx, args, opts...)
		if err == nil {
			return output, nil
		}
		if _, interrupted := compose.IsInterruptRerunError(err); interrupted || ctx.Err() != nil {
			return output, err
		}
		return fmt.Sprintf("Tool %s failed: %v", tCtx.Name, err), nil
	}, nil
}

func unknownToolResult(_ context.Context, name, _ string) (string, error) {
	return fmt.Sprintf("Tool %s does not exist. Call only the tools in your tool list; use tool_search to find deferred tools.", name), nil
}
