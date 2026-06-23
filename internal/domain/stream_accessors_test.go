package domain

import (
	"testing"
)

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
			got := CompactInterruptInfo(tc.in)
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
