package stream

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/domain"
)

func TestStreamSinkContext(t *testing.T) {
	sink := func(item domain.StreamItem) error { return nil }
	ctx := domain.WithStreamSink(context.Background(), sink)
	got := domain.StreamSinkFromContext(ctx)
	if got == nil {
		t.Fatal("expected sink from context")
	}

	// nil sink should not panic
	ctx2 := domain.WithStreamSink(context.Background(), nil)
	if domain.StreamSinkFromContext(ctx2) != nil {
		t.Fatal("nil sink should not be stored")
	}

	// no sink attached
	if domain.StreamSinkFromContext(context.Background()) != nil {
		t.Fatal("no sink should return nil")
	}
}

func TestStreamMessageFromSchema(t *testing.T) {
	msg := &schema.Message{
		Role:             schema.Assistant,
		Content:          "  hello  ",
		ReasoningContent: "reason",
		ToolCallID:       "call_1",
		ToolName:         "test_tool",
		ToolCalls: []schema.ToolCall{
			{ID: "tc1", Function: schema.FunctionCall{Name: "fn1", Arguments: `{"a":1}`}},
		},
	}
	got := StreamMessageFromSchema(msg, "openai")
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.Role != "assistant" {
		t.Fatalf("role = %q", got.Role)
	}
	if got.Content != "hello" {
		t.Fatalf("content not trimmed: %q", got.Content)
	}
	if got.Reasoning != "reason" {
		t.Fatalf("reasoning = %q", got.Reasoning)
	}
	if got.ToolCallID != "call_1" {
		t.Fatalf("tool_call_id = %q", got.ToolCallID)
	}
	if got.ToolName != "test_tool" {
		t.Fatalf("tool_name = %q", got.ToolName)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d", len(got.ToolCalls))
	}
	if got.Meta == nil || got.Meta["active_provider"] != "openai" {
		t.Fatalf("meta = %v", got.Meta)
	}

	// nil message
	if StreamMessageFromSchema(nil, "") != nil {
		t.Fatal("nil message should return nil")
	}
}
