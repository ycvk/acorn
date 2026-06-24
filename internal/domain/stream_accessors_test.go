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
		wantInterrupt bool
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
			name:          "run_interrupted",
			item:          StreamItem{Payload: map[string]any{"interrupt": &StreamInterrupt{ContextCount: 1}}},
			wantInterrupt: true,
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
			if got := ItemGetInterrupt(tc.item); (got != nil) != tc.wantInterrupt {
				t.Fatalf("GetInterrupt() = %v, want %v", got != nil, tc.wantInterrupt)
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
