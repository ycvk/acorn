package eventstream

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestStreamSinkContext(t *testing.T) {
	sink := func(item StreamItem) error { return nil }
	ctx := WithStreamSink(context.Background(), sink)
	got := StreamSinkFromContext(ctx)
	if got == nil {
		t.Fatal("expected sink from context")
	}

	// nil sink should not panic
	ctx2 := WithStreamSink(context.Background(), nil)
	if StreamSinkFromContext(ctx2) != nil {
		t.Fatal("nil sink should not be stored")
	}

	// no sink attached
	if StreamSinkFromContext(context.Background()) != nil {
		t.Fatal("no sink should return nil")
	}
}

func TestAccessors(t *testing.T) {
	cases := []struct {
		name          string
		item          StreamItem
		wantMessage   bool
		wantDelta     bool
		wantTool      bool
		wantSkill     bool
		wantInterrupt bool
		wantMemory    bool
	}{
		{
			name:        "assistant_message",
			item:        StreamItem{Payload: map[string]any{"message": &StreamMessage{Role: "assistant", Content: "hi"}}},
			wantMessage: true,
		},
		{
			name:        "run_completed",
			item:        StreamItem{Payload: map[string]any{"message": &StreamMessage{Role: "assistant", Content: "done"}}},
			wantMessage: true,
		},
		{
			name:      "assistant_delta",
			item:      StreamItem{Payload: map[string]any{"assistant_delta": &StreamAssistantDelta{Delta: "delta"}}},
			wantDelta: true,
		},
		{
			name:     "tool_call_started",
			item:     StreamItem{Payload: map[string]any{"tool_call": &StreamToolCall{Name: "t1"}}},
			wantTool: true,
		},
		{
			name:     "tool_call_succeeded",
			item:     StreamItem{Payload: map[string]any{"tool_call": &StreamToolCall{Name: "t1"}}},
			wantTool: true,
		},
		{
			name:      "skill_discovered",
			item:      StreamItem{Payload: map[string]any{"skill": &StreamSkill{SelectedID: "s1"}}},
			wantSkill: true,
		},
		{
			name:          "run_interrupted",
			item:          StreamItem{Payload: map[string]any{"interrupt": &StreamInterrupt{ContextCount: 1}}},
			wantInterrupt: true,
		},
		{
			name:       "memory_prepared",
			item:       StreamItem{Payload: map[string]any{"memory_prepared": &StreamMemoryPrepared{Query: "ok"}}},
			wantMemory: true,
		},
		{
			name: "no_payload",
			item: StreamItem{Payload: nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.GetMessage(); (got != nil) != tc.wantMessage {
				t.Fatalf("GetMessage() = %v, want %v", got != nil, tc.wantMessage)
			}
			if got := tc.item.GetAssistantDelta(); (got != nil) != tc.wantDelta {
				t.Fatalf("GetAssistantDelta() = %v, want %v", got != nil, tc.wantDelta)
			}
			if got := tc.item.GetToolCall(); (got != nil) != tc.wantTool {
				t.Fatalf("GetToolCall() = %v, want %v", got != nil, tc.wantTool)
			}
			if got := tc.item.GetSkill(); (got != nil) != tc.wantSkill {
				t.Fatalf("GetSkill() = %v, want %v", got != nil, tc.wantSkill)
			}
			if got := tc.item.GetInterrupt(); (got != nil) != tc.wantInterrupt {
				t.Fatalf("GetInterrupt() = %v, want %v", got != nil, tc.wantInterrupt)
			}
			if got := tc.item.GetMemoryPrepared(); (got != nil) != tc.wantMemory {
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
