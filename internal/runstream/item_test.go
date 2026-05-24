package runstream

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStreamItemMarshalJSONRoundtrip(t *testing.T) {
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		item StreamItem
	}{
		{
			name: "run_started",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindRunStarted,
				CreatedAt: now,
				Payload:   &RunStartedPayload{Input: "hello"},
			},
		},
		{
			name: "run_completed_with_message",
			item: StreamItem{
				RunID:     "run_1",
				Sequence:  1,
				Kind:      StreamKindRunCompleted,
				CreatedAt: now,
				Payload: &RunCompletedPayload{
					Message: &StreamMessage{Role: "assistant", Content: "done"},
				},
			},
		},
		{
			name: "tool_call_started",
			item: StreamItem{
				RunID:     "run_1",
				Sequence:  2,
				Kind:      StreamKindToolCallStarted,
				CreatedAt: now,
				Payload: &ToolCallStartedPayload{
					ToolCall: &StreamToolCall{
						CallID:        "call_1",
						Name:          "read_file",
						ArgumentsJSON: `{"path":"main.go"}`,
					},
				},
			},
		},
		{
			name: "context_pressure",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindContextPressure,
				CreatedAt: now,
				Payload: &ContextPressurePayload{
					ContextPressure: &StreamContextPressure{
						State:                      "warning",
						EstimatedInputTokens:       8500,
						EffectiveWindowTokens:      10000,
						WarningThresholdTokens:     8000,
						AutoCompactThresholdTokens: 9000,
						BlockingThresholdTokens:    10000,
						PercentUsed:                85,
					},
				},
			},
		},
		{
			name: "provider_degraded",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindProviderDegraded,
				CreatedAt: now,
				Payload: &ProviderDegradedPayload{
					AffectedProviders: []ProviderDegradedEntry{
						{Name: "openai", Transport: "http", Error: "timeout"},
					},
				},
			},
		},
		{
			name: "mcp_provider_added",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindMCPProviderAdded,
				CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "filesystem",
					streamKind:   StreamKindMCPProviderAdded,
				},
			},
		},
		{
			name: "nil_payload",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindRunStarted,
				CreatedAt: now,
				Payload:   nil,
			},
		},
		{
			name: "elicitation_pending",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindElicitationPending,
				CreatedAt: now,
				Payload:   &ElicitationPayload{ActionID: "act_1", Message: "confirm"},
			},
		},
		{
			name: "sampling_started",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindSamplingStarted,
				CreatedAt: now,
				Payload:   &SamplingPayload{RunID: "run_1", Model: "gpt-4"},
			},
		},
		{
			name: "plan_created",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindPlanCreated,
				CreatedAt: now,
				Payload:   &PlanCreatedPayload{Plan: &StreamPlan{PlanID: "plan_1", RunID: "run_1", Steps: []PlanStep{}}},
			},
		},
		{
			name: "crystallization_verdict",
			item: StreamItem{
				RunID:     "run_1",
				Kind:      StreamKindCrystallizationVerdict,
				CreatedAt: now,
				Payload:   &CrystallizationVerdictPayload{RunID: "run_1", Verdict: "crystallized", SkillID: "skill_1"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.item)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var got StreamItem
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got.RunID != tc.item.RunID {
				t.Fatalf("run_id = %q, want %q", got.RunID, tc.item.RunID)
			}
			if got.Kind != tc.item.Kind {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.item.Kind)
			}
			if got.Sequence != tc.item.Sequence {
				t.Fatalf("sequence = %d, want %d", got.Sequence, tc.item.Sequence)
			}
			if !got.CreatedAt.Equal(tc.item.CreatedAt) {
				t.Fatalf("created_at = %v, want %v", got.CreatedAt, tc.item.CreatedAt)
			}

			if tc.item.Payload == nil {
				if got.Payload != nil {
					t.Fatalf("expected nil payload, got %T", got.Payload)
				}
				return
			}

			if got.Payload == nil {
				t.Fatalf("expected payload, got nil")
			}
			if got.Payload.StreamKind() != tc.item.Payload.StreamKind() {
				t.Fatalf("payload kind = %q, want %q", got.Payload.StreamKind(), tc.item.Payload.StreamKind())
			}
		})
	}
}

func TestStreamItemUnmarshalJSONMissingFields(t *testing.T) {
	_, err := json.Marshal(StreamItem{Kind: StreamKindRunStarted})
	if err != nil {
		t.Fatalf("marshal item without run_id: %v", err)
	}

	var item StreamItem
	if err := json.Unmarshal([]byte(`{"kind":"run_started"}`), &item); err == nil {
		t.Fatalf("expected error for missing run_id, got nil")
	}

	if err := json.Unmarshal([]byte(`{"run_id":"run_1"}`), &item); err == nil {
		t.Fatalf("expected error for missing kind, got nil")
	}
}

func TestStreamItemUnmarshalJSONUnknownKind(t *testing.T) {
	var item StreamItem
	if err := json.Unmarshal([]byte(`{"run_id":"run_1","kind":"unknown_kind_xyz","created_at":"2026-05-24T10:00:00Z","extra_field":"value"}`), &item); err == nil {
		t.Fatalf("expected error for unknown kind, got nil")
	}
}

func TestStreamItemUnmarshalJSONInvalidCreatedAt(t *testing.T) {
	var item StreamItem
	if err := json.Unmarshal([]byte(`{"run_id":"run_1","kind":"run_started","created_at":"not-a-date"}`), &item); err == nil {
		t.Fatalf("expected error for invalid created_at, got nil")
	}
}

func TestStreamItemMarshalJSONOmitsZeroSequence(t *testing.T) {
	item := StreamItem{RunID: "run_1", Kind: StreamKindRunStarted, CreatedAt: time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["sequence"]; ok {
		t.Fatalf("expected sequence to be omitted when zero, got %v", m["sequence"])
	}
}

func TestUnmarshalPayloadAllKinds(t *testing.T) {
	cases := []struct {
		kind    StreamItemKind
		payload StreamPayload
	}{
		{StreamKindRunStarted, &RunStartedPayload{}},
		{StreamKindRunCompleted, &RunCompletedPayload{}},
		{StreamKindRunFailed, &RunFailedPayload{}},
		{StreamKindRunInterrupted, &RunInterruptedPayload{}},
		{StreamKindRunResumeRequested, &RunResumeRequestedPayload{}},
		{StreamKindDecisionSelected, &DecisionSelectedPayload{}},
		{StreamKindDecisionBlocked, &DecisionBlockedPayload{}},
		{StreamKindSkillDiscovered, &SkillDiscoveredPayload{}},
		{StreamKindSkillSelected, &SkillSelectedPayload{}},
		{StreamKindSkillLoaded, &SkillLoadedPayload{}},
		{StreamKindSkillFailed, &SkillFailedPayload{}},
		{StreamKindSkillLifecycle, &SkillLifecyclePayload{}},
		{StreamKindProcedureActivation, &ProcedureActivationPayload{}},
		{StreamKindMemoryPrepared, &MemoryPreparedPayload{}},
		{StreamKindContextPressure, &ContextPressurePayload{}},
		{StreamKindContextCompressed, &ContextCompressedPayload{}},
		{StreamKindAssistantDelta, &AssistantDeltaPayload{}},
		{StreamKindAssistantMessage, &AssistantMessagePayload{}},
		{StreamKindToolCallStarted, &ToolCallStartedPayload{}},
		{StreamKindToolCallProgress, &ToolCallProgressPayload{}},
		{StreamKindToolCallSucceeded, &ToolCallSucceededPayload{}},
		{StreamKindToolCallFailed, &ToolCallFailedPayload{}},
		{StreamKindToolCallInterrupted, &ToolCallInterruptedPayload{}},
		{StreamKindProviderDegraded, &ProviderDegradedPayload{}},
		{StreamKindMCPToolCatalogRefreshed, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPProviderAdded, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPProviderRemoved, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPProviderRestarted, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPResourceCatalogRefreshed, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPResourceCatalogRefreshFailed, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPPromptCatalogRefreshed, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPPromptCatalogRefreshFailed, &MCPProviderLifecyclePayload{}},
		{StreamKindMCPAuthStatusChanged, &MCPProviderLifecyclePayload{}},
		{StreamKindElicitationPending, &ElicitationPayload{}},
		{StreamKindElicitationDecided, &ElicitationPayload{}},
		{StreamKindSamplingStarted, &SamplingPayload{}},
		{StreamKindSamplingCompleted, &SamplingPayload{}},
		{StreamKindSamplingFailed, &SamplingPayload{}},
		{StreamKindSubagentStarted, &SubagentStartedPayload{}},
		{StreamKindSubagentCompleted, &SubagentCompletedPayload{}},
		{StreamKindSubagentFailed, &SubagentFailedPayload{}},
		{StreamKindHeartbeat, &HeartbeatPayload{}},
		{StreamKindToolParallelBatchStarted, &ToolParallelBatchStartedPayload{}},
		{StreamKindToolParallelBatchCompleted, &ToolParallelBatchCompletedPayload{}},
		{StreamKindRunArchived, &RunArchivedPayload{}},
		{StreamKindPlanCreated, &PlanCreatedPayload{}},
		{StreamKindPlanUpdated, &PlanUpdatedPayload{}},
		{StreamKindPlanCleared, &PlanClearedPayload{}},
		{StreamKindStepStarted, &PlanStepStartedPayload{}},
		{StreamKindStepCompleted, &PlanStepCompletedPayload{}},
		{StreamKindStepFailed, &PlanStepFailedPayload{}},
		{StreamKindCrystallizationFailed, &CrystallizationFailedPayload{}},
		{StreamKindCrystallizationVerdict, &CrystallizationVerdictPayload{}},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			data, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			p, err := UnmarshalPayload(tc.kind, data)
			if err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if p.StreamKind() != tc.kind {
				t.Fatalf("kind = %q, want %q", p.StreamKind(), tc.kind)
			}
		})
	}
}

func TestUnmarshalPayloadUnknownKind(t *testing.T) {
	_, err := UnmarshalPayload("unknown_xyz", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestUnmarshalPayloadInvalidJSON(t *testing.T) {
	_, err := UnmarshalPayload(StreamKindRunStarted, []byte(`{invalid`))
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestMCPProviderLifecyclePayloadStreamKind(t *testing.T) {
	p := &MCPProviderLifecyclePayload{ProviderName: "test"}
	if p.StreamKind() != StreamKindMCPToolCatalogRefreshed {
		t.Fatalf("default kind = %q, want %q", p.StreamKind(), StreamKindMCPToolCatalogRefreshed)
	}

	p2 := MCPProviderLifecyclePayload{ProviderName: "test", streamKind: StreamKindMCPProviderAdded}
	if p2.StreamKind() != StreamKindMCPProviderAdded {
		t.Fatalf("kind = %q, want %q", p2.StreamKind(), StreamKindMCPProviderAdded)
	}
}

func TestElicitationPayloadStreamKind(t *testing.T) {
	p := ElicitationPayload{ActionID: "act_1", Message: "test"}
	if p.StreamKind() != StreamKindElicitationPending {
		t.Fatalf("default kind = %q, want %q", p.StreamKind(), StreamKindElicitationPending)
	}

	p2 := ElicitationPayloadWithStreamKind(p, StreamKindElicitationDecided)
	if p2.StreamKind() != StreamKindElicitationDecided {
		t.Fatalf("kind = %q, want %q", p2.StreamKind(), StreamKindElicitationDecided)
	}
}

func TestSamplingPayloadStreamKind(t *testing.T) {
	p := SamplingPayload{RunID: "run_1"}
	if p.StreamKind() != StreamKindSamplingStarted {
		t.Fatalf("default kind = %q, want %q", p.StreamKind(), StreamKindSamplingStarted)
	}

	p2 := SamplingPayloadWithStreamKind(p, StreamKindSamplingCompleted)
	if p2.StreamKind() != StreamKindSamplingCompleted {
		t.Fatalf("kind = %q, want %q", p2.StreamKind(), StreamKindSamplingCompleted)
	}
}
