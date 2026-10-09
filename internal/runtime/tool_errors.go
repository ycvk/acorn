package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/schema"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

// failedToolCalls holds the tool calls of one run whose error became a tool
// result, keyed by call id, so the projector can report them as failed.
type failedToolCalls struct {
	mu    sync.Mutex
	calls map[string]string
}

func newFailedToolCalls() *failedToolCalls {
	return &failedToolCalls{calls: map[string]string{}}
}

func (f *failedToolCalls) record(callID, errText string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[callID] = errText
}

// take returns and forgets the error recorded for callID.
func (f *failedToolCalls) take(callID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	errText, ok := f.calls[callID]
	delete(f.calls, callID)
	return errText, ok
}

// toolErrorMiddleware turns ordinary tool failures into tool results the model
// can read and react to. Interrupts and run cancellation still propagate, so
// approvals pause the run and a cancelled run stops.
type toolErrorMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	failed *failedToolCalls
}

func newToolErrorMiddleware(failed *failedToolCalls) *toolErrorMiddleware {
	return &toolErrorMiddleware{TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{}, failed: failed}
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
		m.failed.record(tCtx.CallID, err.Error())
		return fmt.Sprintf("Tool %s failed: %v", tCtx.Name, err), nil
	}, nil
}

func unknownToolResult(_ context.Context, name, _ string) (string, error) {
	return fmt.Sprintf("Tool %s does not exist. Call only the tools in your tool list; use tool_search to find deferred tools.", name), nil
}
