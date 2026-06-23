package tooldispatch

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// StreamingExecutor submits tool calls and collects results in streaming fashion.
type StreamingExecutor interface {
	Submit(call schema.ToolCall)
	GetRemainingResults(ctx context.Context) ([]*schema.Message, error)
	Discard()
}

// ToolInvoker creates streaming executors for parallel tool execution.
type ToolInvoker interface {
	NewStreamingExecutor(ctx context.Context) StreamingExecutor
}

// ToolAuditCallIDKey is the context key for the current tool call ID.
type ToolAuditCallIDKey struct{}

func WithToolAuditCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, ToolAuditCallIDKey{}, strings.TrimSpace(callID))
}

// ToolAuditCallID retrieves the tool call ID from the context.
func ToolAuditCallID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(ToolAuditCallIDKey{}).(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}
