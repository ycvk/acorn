package stream

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
				Payload:   RunStartedPayload{Input: "hello"},
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
				Payload: ToolCallSucceededPayload{
					ToolCall: &StreamToolCall{
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
				Kind:      StreamKindHeartbeat,
				CreatedAt: now,
			},
			want: `"kind":"stream.heartbeat"`,
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
			if got.Payload != nil {
				if got.Payload.StreamKind() != tc.item.Payload.StreamKind() {
					t.Fatalf("payload kind: got %q, want %q", got.Payload.StreamKind(), tc.item.Payload.StreamKind())
				}
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

func TestUnmarshalPayloadKnownKinds(t *testing.T) {
	cases := []struct {
		kind StreamItemKind
		data string
	}{
		{StreamKindRunStarted, `{"input":"hello"}`},
		{StreamKindRunCompleted, `{}`},
		{StreamKindRunFailed, `{"error":"boom"}`},
		{StreamKindDecisionSelected, `{"action":"plan"}`},
		{StreamKindSkillDiscovered, `{}`},
		{StreamKindSkillSelected, `{}`},
		{StreamKindSkillLoaded, `{}`},
		{StreamKindSkillFailed, `{}`},
		{StreamKindSkillLifecycle, `{}`},
		{StreamKindProcedureActivation, `{}`},
		{StreamKindMemoryPrepared, `{}`},
		{StreamKindContextPressure, `{}`},
		{StreamKindContextCompressed, `{}`},
		{StreamKindAssistantDelta, `{}`},
		{StreamKindAssistantMessage, `{}`},
		{StreamKindToolCallStarted, `{"call_id":"c1","tool_name":"t1"}`},
		{StreamKindToolCallProgress, `{}`},
		{StreamKindToolCallSucceeded, `{}`},
		{StreamKindToolCallFailed, `{}`},
		{StreamKindToolCallInterrupted, `{}`},
		{StreamKindProviderDegraded, `{}`},
		{StreamKindMCPToolCatalogRefreshed, `{}`},
		{StreamKindMCPProviderAdded, `{}`},
		{StreamKindMCPResourceCatalogRefreshed, `{}`},
		{StreamKindMCPPromptCatalogRefreshed, `{}`},
		{StreamKindMCPAuthStatusChanged, `{}`},
		{StreamKindElicitationPending, `{"action_id":"a1"}`},
		{StreamKindSamplingStarted, `{"run_id":"r1"}`},
		{StreamKindSubagentStarted, `{}`},
		{StreamKindSubagentCompleted, `{}`},
		{StreamKindSubagentFailed, `{}`},
		{StreamKindHeartbeat, `{}`},
		{StreamKindToolParallelBatchStarted, `{}`},
		{StreamKindToolParallelBatchCompleted, `{}`},
		{StreamKindRunArchived, `{"run_id":"r1","events_compressed":0}`},
		{StreamKindPlanCreated, `{}`},
		{StreamKindPlanUpdated, `{}`},
		{StreamKindPlanCleared, `{}`},
		{StreamKindStepStarted, `{}`},
		{StreamKindStepCompleted, `{}`},
		{StreamKindStepFailed, `{}`},
		{StreamKindCrystallizationFailed, `{}`},
		{StreamKindCrystallizationVerdict, `{}`},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			p, err := UnmarshalPayload(tc.kind, json.RawMessage(tc.data))
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p == nil {
				t.Fatal("payload is nil")
			}
			if p.StreamKind() != tc.kind {
				t.Fatalf("kind mismatch: got %q, want %q", p.StreamKind(), tc.kind)
			}
		})
	}
}

func TestUnmarshalPayloadUnknownKind(t *testing.T) {
	_, err := UnmarshalPayload("unknown.kind", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestPayloadStreamKindMethods(t *testing.T) {
	cases := []struct {
		payload StreamPayload
		want    StreamItemKind
	}{
		{RunStartedPayload{}, StreamKindRunStarted},
		{RunCompletedPayload{}, StreamKindRunCompleted},
		{RunFailedPayload{}, StreamKindRunFailed},
		{RunInterruptedPayload{}, StreamKindRunInterrupted},
		{RunResumeRequestedPayload{}, StreamKindRunResumeRequested},
		{RunArchivedPayload{}, StreamKindRunArchived},
		{DecisionSelectedPayload{}, StreamKindDecisionSelected},
		{DecisionBlockedPayload{}, StreamKindDecisionBlocked},
		{SkillDiscoveredPayload{}, StreamKindSkillDiscovered},
		{SkillSelectedPayload{}, StreamKindSkillSelected},
		{SkillLoadedPayload{}, StreamKindSkillLoaded},
		{SkillFailedPayload{}, StreamKindSkillFailed},
		{SkillLifecyclePayload{}, StreamKindSkillLifecycle},
		{ProcedureActivationPayload{}, StreamKindProcedureActivation},
		{MemoryPreparedPayload{}, StreamKindMemoryPrepared},
		{ContextPressurePayload{}, StreamKindContextPressure},
		{ContextCompressedPayload{}, StreamKindContextCompressed},
		{AssistantDeltaPayload{}, StreamKindAssistantDelta},
		{AssistantMessagePayload{}, StreamKindAssistantMessage},
		{ToolCallStartedPayload{}, StreamKindToolCallStarted},
		{ToolCallProgressPayload{}, StreamKindToolCallProgress},
		{ToolCallSucceededPayload{}, StreamKindToolCallSucceeded},
		{ToolCallFailedPayload{}, StreamKindToolCallFailed},
		{ToolCallInterruptedPayload{}, StreamKindToolCallInterrupted},
		{ProviderDegradedPayload{}, StreamKindProviderDegraded},
		{MCPProviderLifecyclePayload{}, StreamKindMCPToolCatalogRefreshed},
		{ElicitationPayload{}, StreamKindElicitationPending},
		{SamplingPayload{}, StreamKindSamplingStarted},
		{SubagentStartedPayload{}, StreamKindSubagentStarted},
		{SubagentCompletedPayload{}, StreamKindSubagentCompleted},
		{SubagentFailedPayload{}, StreamKindSubagentFailed},
		{HeartbeatPayload{}, StreamKindHeartbeat},
		{ToolParallelBatchStartedPayload{}, StreamKindToolParallelBatchStarted},
		{ToolParallelBatchCompletedPayload{}, StreamKindToolParallelBatchCompleted},
		{&PlanCreatedPayload{}, StreamKindPlanCreated},
		{&PlanUpdatedPayload{}, StreamKindPlanUpdated},
		{&PlanClearedPayload{}, StreamKindPlanCleared},
		{&PlanStepStartedPayload{}, StreamKindStepStarted},
		{&PlanStepCompletedPayload{}, StreamKindStepCompleted},
		{&PlanStepFailedPayload{}, StreamKindStepFailed},
		{&CrystallizationFailedPayload{}, StreamKindCrystallizationFailed},
		{&CrystallizationVerdictPayload{}, StreamKindCrystallizationVerdict},
	}

	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			if got := tc.payload.StreamKind(); got != tc.want {
				t.Fatalf("StreamKind() = %q, want %q", got, tc.want)
			}
		})
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
