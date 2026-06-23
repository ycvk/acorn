package stream

import (
	"context"

	"testing"

	"github.com/ycvk/acorn/internal/domain"

	"github.com/cloudwego/eino/schema"
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

func TestAccessors(t *testing.T) {
	cases := []struct {
		name          string
		item          domain.StreamItem
		wantMessage   bool
		wantDelta     bool
		wantTool      bool
		wantSkill     bool
		wantInterrupt bool
		wantMemory    bool
	}{
		{
			name:        "assistant_message",
			item:        domain.StreamItem{Payload: map[string]any{"message": &StreamMessage{Role: "assistant", Content: "hi"}}},
			wantMessage: true,
		},
		{
			name:        "run_completed",
			item:        domain.StreamItem{Payload: map[string]any{"message": &StreamMessage{Role: "assistant", Content: "done"}}},
			wantMessage: true,
		},
		{
			name:      "assistant_delta",
			item:      domain.StreamItem{Payload: map[string]any{"assistant_delta": &StreamAssistantDelta{Delta: "delta"}}},
			wantDelta: true,
		},
		{
			name:     "tool_call_started",
			item:     domain.StreamItem{Payload: map[string]any{"tool_call": &StreamToolCall{Name: "t1"}}},
			wantTool: true,
		},
		{
			name:     "tool_call_succeeded",
			item:     domain.StreamItem{Payload: map[string]any{"tool_call": &StreamToolCall{Name: "t1"}}},
			wantTool: true,
		},
		{
			name:      "skill_discovered",
			item:      domain.StreamItem{Payload: map[string]any{"skill": &StreamSkill{SelectedID: "s1"}}},
			wantSkill: true,
		},
		{
			name:          "run_interrupted",
			item:          domain.StreamItem{Payload: map[string]any{"interrupt": &StreamInterrupt{ContextCount: 1}}},
			wantInterrupt: true,
		},
		{
			name:       "memory_prepared",
			item:       domain.StreamItem{Payload: map[string]any{"memory_prepared": &StreamMemoryPrepared{Query: "ok"}}},
			wantMemory: true,
		},
		{
			name: "no_payload",
			item: domain.StreamItem{Payload: nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ItemGetMessage(tc.item); (got != nil) != tc.wantMessage {
				t.Fatalf("GetMessage() = %v, want %v", got != nil, tc.wantMessage)
			}
			if got := ItemGetAssistantDelta(tc.item); (got != nil) != tc.wantDelta {
				t.Fatalf("GetAssistantDelta() = %v, want %v", got != nil, tc.wantDelta)
			}
			if got := ItemGetToolCall(tc.item); (got != nil) != tc.wantTool {
				t.Fatalf("GetToolCall() = %v, want %v", got != nil, tc.wantTool)
			}
			if got := ItemGetSkill(tc.item); (got != nil) != tc.wantSkill {
				t.Fatalf("GetSkill() = %v, want %v", got != nil, tc.wantSkill)
			}
			if got := ItemGetInterrupt(tc.item); (got != nil) != tc.wantInterrupt {
				t.Fatalf("GetInterrupt() = %v, want %v", got != nil, tc.wantInterrupt)
			}
			if got := ItemGetMemoryPrepared(tc.item); (got != nil) != tc.wantMemory {
				t.Fatalf("GetMemoryPrepared() = %v, want %v", got != nil, tc.wantMemory)
			}
		})
	}
}

func TestCompactInterruptInfo(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want map[string]any
	}{
		{
			name: "valid keys",
			in:   map[string]any{"kind": "action", "message": "hello", "extra": "ignored"},
			want: map[string]any{"kind": "action", "message": "hello"},
		},
		{
			name: "non-map",
			in:   "string",
			want: nil,
		},
		{
			name: "empty after filter",
			in:   map[string]any{"extra": "ignored"},
			want: nil,
		},
		{
			name: "nil",
			in:   nil,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compactInterruptInfo(tc.in)
			if got == nil && tc.want == nil {
				return
			}
			gm, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("got non-map: %v", got)
			}
			if len(gm) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(gm), len(tc.want))
			}
			for k, v := range tc.want {
				if gm[k] != v {
					t.Fatalf("%s = %v, want %v", k, gm[k], v)
				}
			}
		})
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
