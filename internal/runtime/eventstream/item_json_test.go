package eventstream

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStreamItemMarshalUnmarshalRoundTrip(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		item StreamItem
		want string // expected JSON subset
	}{
		{
			name: "run_started with payload",
			item: StreamItem{
				RunID:     "run_1",
				Sequence:  1,
				Kind:      StreamKindRunStarted,
				CreatedAt: now,
				Payload:   map[string]any{"input": "hello"},
			},
			want: `"input":"hello"`,
		},
		{
			name: "tool_call_succeeded with message",
			item: StreamItem{
				RunID:     "run_1",
				Sequence:  5,
				Kind:      StreamKindToolCallSucceeded,
				CreatedAt: now,
				Payload: map[string]any{
					"tool_call": &StreamToolCall{
						CallID: "call_1",
						Name:   "test_tool",
					},
				},
			},
			want: `"name":"test_tool"`,
		},
		{
			name: "plain item without payload",
			item: StreamItem{
				RunID:     "run_2",
				Kind:      StreamKindRunCompleted,
				CreatedAt: now,
			},
			want: `"kind":"run_completed"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.item)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !contains(string(b), tc.want) {
				t.Fatalf("marshaled JSON missing %q: %s", tc.want, b)
			}

			var got StreamItem
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.RunID != tc.item.RunID {
				t.Fatalf("run_id: got %q, want %q", got.RunID, tc.item.RunID)
			}
			if got.Kind != tc.item.Kind {
				t.Fatalf("kind: got %q, want %q", got.Kind, tc.item.Kind)
			}
			if got.Sequence != tc.item.Sequence {
				t.Fatalf("sequence: got %d, want %d", got.Sequence, tc.item.Sequence)
			}
			if got.Payload == nil && tc.item.Payload != nil {
				t.Fatal("payload was lost")
			}
		})
	}
}

func TestStreamItemUnmarshalMissingRunID(t *testing.T) {
	data := []byte(`{"kind":"run_started","created_at":"2024-01-01T00:00:00Z"}`)
	var item StreamItem
	if err := json.Unmarshal(data, &item); err == nil {
		t.Fatal("expected error for missing run_id")
	}
}

func TestStreamItemUnmarshalMissingKind(t *testing.T) {
	data := []byte(`{"run_id":"run_1","created_at":"2024-01-01T00:00:00Z"}`)
	var item StreamItem
	if err := json.Unmarshal(data, &item); err == nil {
		t.Fatal("expected error for missing kind")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
