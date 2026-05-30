package runtime

import (
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/stream"
)

func TestStreamProjectionRoundtrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		item stream.StreamItem
	}{
		{
			name: "run_started",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 1, Kind: stream.StreamKindRunStarted, CreatedAt: now,
				Payload: map[string]any{"input": "inspect the codebase"},
			},
		},
		{
			name: "run_completed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 2, Kind: stream.StreamKindRunCompleted, CreatedAt: now,
				Payload: map[string]any{"message": &stream.StreamMessage{
					Role:      "assistant",
					Content:   "done",
					Reasoning: "thought process",
					ToolCalls: []stream.StreamPlannedToolCall{
						{ID: "tc_1", Name: "read_file", ArgumentsJSON: `{"path":"README.md"}`},
					},
					Meta: map[string]any{"active_provider": "primary", "latency_ms": 150},
				}},
			},
		},
		{
			name: "run_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 3, Kind: stream.StreamKindRunFailed, CreatedAt: now,
				Payload: map[string]any{"error": "model unavailable: connection refused"},
			},
		},
		{
			name: "run_interrupted",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 4, Kind: stream.StreamKindRunInterrupted, CreatedAt: now,
				Payload: map[string]any{"interrupt": &stream.StreamInterrupt{
					ContextCount: 2,
					Contexts: []stream.StreamInterruptContext{
						{ID: "int_1", Address: "tool.run_command", Info: map[string]any{"kind": "approval", "cmd": "rm -rf /"}, IsRootCause: true},
						{ID: "int_2", Address: "tool.create_file", Info: map[string]any{"kind": "approval"}, IsRootCause: false},
					},
				}},
			},
		},
		{
			name: "run_resume_requested",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 5, Kind: stream.StreamKindRunResumeRequested, CreatedAt: now,
				Payload: map[string]any{"targets": map[string]any{
					"interrupt_ids": []any{"int_1"},
					"approved":      true,
					"count":         1,
				}},
			},
		},
		{
			name: "decision_selected",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 7, Kind: stream.StreamKindDecisionSelected, CreatedAt: now,
				Payload: map[string]any{"action": "skill", "intent": "inspect the repo", "selected_skill_id": "skill.inspect.repo", "decision_reason": "profile_route", "decision_profile_hash": "abc123", "explicit_skill_id": "skill.inspect.repo"},
			},
		},
		{
			name: "decision_blocked",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 8, Kind: stream.StreamKindDecisionBlocked, CreatedAt: now,
				Payload: map[string]any{"action": "skill", "intent": "deploy to prod", "selected_skill_id": "", "decision_reason": "missing_required_capability", "decision_profile_hash": "def456", "explicit_skill_id": ""},
			},
		},

		{
			name: "skill_discovered",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 9, Kind: stream.StreamKindSkillDiscovered, CreatedAt: now,
				Payload: map[string]any{"skill": &stream.StreamSkill{
					SelectedID: "",
					Name:       "Inspect Repo",
					Source:     "workspace",
					Candidates: []stream.StreamSkillCandidate{
						{ID: "skill.inspect.repo", Name: "Inspect Repo", Score: 145, MatchedTerms: []string{"inspect", "repo"}, Summary: "Quick repo overview", Requirements: stream.StreamSkillRequirements{Tools: []string{"read_file", "run_command"}}},
					},
					NoSelectionReason: "ambiguous_top_score",
				}},
			},
		},
		{
			name: "skill_selected",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 10, Kind: stream.StreamKindSkillSelected, CreatedAt: now,
				Payload: map[string]any{"skill": &stream.StreamSkill{
					SelectedID:   "skill.inspect.repo",
					Name:         "Inspect Repo",
					Source:       "workspace",
					Path:         "/tmp/skills/inspect_repo",
					Instruction:  "Read README.md first.",
					Scripts:      []string{"scripts/quick_map.sh"},
					Requirements: stream.StreamSkillRequirements{Tools: []string{"read_file", "run_command"}, Toolsets: []string{"fs"}, Bins: []string{"git"}, Env: []string{"HOME"}},
					Score:        145,
					MatchedTerms: []string{"inspect", "repo"},
				}},
			},
		},
		{
			name: "skill_loaded",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 11, Kind: stream.StreamKindSkillLoaded, CreatedAt: now,
				Payload: map[string]any{"skill": &stream.StreamSkill{
					SelectedID:   "skill.inspect.repo",
					Name:         "Inspect Repo",
					Source:       "workspace",
					Path:         "/tmp/skills/inspect_repo",
					Instruction:  "Read README.md first.",
					Summary:      "Quick repo overview",
					RunStatus:    "active",
					PromotedFrom: "discovered",
				}},
			},
		},
		{
			name: "skill_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 12, Kind: stream.StreamKindSkillFailed, CreatedAt: now,
				Payload: map[string]any{"skill": &stream.StreamSkill{
					SelectedID:    "skill.inspect.repo",
					Name:          "Inspect Repo",
					FailureReason: "missing_tool_use:read_file",
				}},
			},
		},
		{
			name: "skill_lifecycle",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 13, Kind: stream.StreamKindSkillLifecycle, CreatedAt: now,
				Payload: map[string]any{"skill_lifecycle": &stream.StreamSkillLifecycle{
					SkillID:         "skill.generated",
					Action:          "assessed",
					Status:          "verified",
					Verdict:         "verified",
					Reason:          "durable evidence-backed promotion",
					EvidenceRefs:    []string{"child_run:run_eval"},
					AssessmentID:    "skill_assessment_1",
					ChangesRequired: []string{"none"},
					Applied:         true,
				}},
			},
		},
		{
			name: "assistant_message",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 19, Kind: stream.StreamKindAssistantMessage, CreatedAt: now,
				Payload: map[string]any{"message": &stream.StreamMessage{
					Role:      "assistant",
					Content:   "I'll read the README for you.",
					Reasoning: "User wants repo overview",
					ToolCalls: []stream.StreamPlannedToolCall{
						{ID: "tc_1", Name: "read_file", ArgumentsJSON: `{"path":"README.md"}`},
					},
					Meta: map[string]any{"active_provider": "primary"},
				}},
			},
		},
		{
			name: "tool_call_started",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 19, Kind: stream.StreamKindToolCallStarted, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:      "local",
					Name:          "read_file",
					ArgumentsJSON: `{"path":"README.md"}`,
				}},
			},
		},
		{
			name: "tool_call_succeeded",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 20, Kind: stream.StreamKindToolCallSucceeded, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:   "local",
					Name:       "read_file",
					Output:     "# Acorn\n\nA Go-based agent runtime.",
					DurationMS: 120,
				}},
			},
		},
		{
			name: "tool_call_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 22, Kind: stream.StreamKindToolCallFailed, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:   "local",
					Name:       "run_command",
					Error:      "exit status 1: command not found",
					DurationMS: 50,
				}},
			},
		},
		{
			name: "tool_call_interrupted",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 23, Kind: stream.StreamKindToolCallInterrupted, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:          "local",
					Name:              "run_command",
					InterruptID:       "int_1",
					InterruptContexts: 1,
				}},
			},
		},
		{
			name: "elicitation.pending",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 47, Kind: stream.StreamKindElicitationPending, CreatedAt: now,
				Payload: map[string]any{"action_id": "act_1", "message": "Please approve shell execution", "requested_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"approved": map[string]any{"type": "boolean", "description": "Approve the action"},
					},
					"required": []any{"approved"},
				}},
			},
		},
		{
			name: "elicitation.decided",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 48, Kind: stream.StreamKindElicitationDecided, CreatedAt: now,
				Payload: map[string]any{"action_id": "act_1", "message": "User approved", "requested_schema": map[string]any{"type": "object"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eventKind, payload, err := stream.ProjectStreamItemToEvent(tt.item)
			if err != nil {
				t.Fatalf("forward projection failed: %v", err)
			}
			if m, ok := payload.(map[string]any); ok {
				if _, exists := m["tool_call"]; exists {
					t.Fatalf("tool_call key should have been removed from event payload map, but it exists: %#v", m)
				}
			}

			event := events.EventRecord{
				Sequence:  tt.item.Sequence,
				RunID:     tt.item.RunID,
				Kind:      eventKind,
				CreatedAt: tt.item.CreatedAt,
				Payload:   payload,
			}

			result := mustProjectEventToStreamItem(t, event)

			if result.Kind != tt.item.Kind {
				t.Fatalf("kind mismatch: got %q, want %q", result.Kind, tt.item.Kind)
			}

			if result.Payload == nil {
				t.Fatalf("payload is nil after roundtrip")
			}

			assertStreamItemsEqualJSON(t, tt.item, result)
		})
	}
}

func TestStreamProjectionRoundtrip_NilOptionalFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		item stream.StreamItem
	}{
		{
			name: "run_completed_nil_message",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 1, Kind: stream.StreamKindRunCompleted, CreatedAt: now,
				Payload: map[string]any{"message": nil},
			},
		},
		{
			name: "run_interrupted_nil_interrupt",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 2, Kind: stream.StreamKindRunInterrupted, CreatedAt: now,
				Payload: map[string]any{"interrupt": nil},
			},
		},
		{
			name: "skill_discovered_nil_skill",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 3, Kind: stream.StreamKindSkillDiscovered, CreatedAt: now,
				Payload: map[string]any{"skill": nil},
			},
		},
		{
			name: "assistant_message_nil_message",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 4, Kind: stream.StreamKindAssistantMessage, CreatedAt: now,
				Payload: map[string]any{"message": nil},
			},
		},
		{
			name: "tool_call_started_nil_toolcall",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 5, Kind: stream.StreamKindToolCallStarted, CreatedAt: now,
				Payload: map[string]any{"tool_call": nil},
			},
		},
		{
			name: "elicitation_nil_schema",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 9, Kind: stream.StreamKindElicitationPending, CreatedAt: now,
				Payload: map[string]any{"action_id": "act_nil", "message": "no schema"},
			},
		},
		{
			name: "run_resume_requested_nil_targets",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 10, Kind: stream.StreamKindRunResumeRequested, CreatedAt: now,
				Payload: map[string]any{"targets": nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eventKind, payload, err := stream.ProjectStreamItemToEvent(tt.item)
			if err != nil {
				t.Fatalf("forward projection failed: %v", err)
			}

			event := events.EventRecord{
				Sequence:  tt.item.Sequence,
				RunID:     tt.item.RunID,
				Kind:      eventKind,
				CreatedAt: tt.item.CreatedAt,
				Payload:   payload,
			}

			result := mustProjectEventToStreamItem(t, event)

			if result.Kind != tt.item.Kind {
				t.Fatalf("kind mismatch: got %q, want %q", result.Kind, tt.item.Kind)
			}

			assertStreamItemsEqualJSON(t, tt.item, result)
		})
	}
}

func TestStreamProjectionRoundtrip_ToolCallMerge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		item stream.StreamItem
	}{
		{
			name: "tool_call_started_full",
			item: stream.StreamItem{
				RunID: "run_tc", Sequence: 1, Kind: stream.StreamKindToolCallStarted, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:          "local",
					Name:              "run_command",
					ArgumentsJSON:     `{"cmd":"ls -la"}`,
					InterruptID:       "int_1",
					Output:            "",
					Error:             "",
					DurationMS:        0,
					InterruptContexts: 0,
				}},
			},
		},
		{
			name: "tool_call_succeeded_full",
			item: stream.StreamItem{
				RunID: "run_tc", Sequence: 2, Kind: stream.StreamKindToolCallSucceeded, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:   "mcp.remote",
					Name:       "read_file",
					Output:     "file contents here",
					DurationMS: 200,
				}},
			},
		},
		{
			name: "tool_call_failed_full",
			item: stream.StreamItem{
				RunID: "run_tc", Sequence: 4, Kind: stream.StreamKindToolCallFailed, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:   "local",
					Name:       "run_command",
					Error:      "exit status 127",
					DurationMS: 10,
				}},
			},
		},
		{
			name: "tool_call_interrupted_full",
			item: stream.StreamItem{
				RunID: "run_tc", Sequence: 5, Kind: stream.StreamKindToolCallInterrupted, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCall{
					Provider:          "local",
					Name:              "run_command",
					InterruptID:       "int_2",
					InterruptContexts: 2,
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eventKind, payload, err := stream.ProjectStreamItemToEvent(tt.item)
			if err != nil {
				t.Fatalf("forward projection failed: %v", err)
			}

			event := events.EventRecord{
				Sequence:  tt.item.Sequence,
				RunID:     tt.item.RunID,
				Kind:      eventKind,
				CreatedAt: tt.item.CreatedAt,
				Payload:   payload,
			}

			result := mustProjectEventToStreamItem(t, event)

			if result.GetToolCall() == nil {
				t.Fatalf("ToolCall is nil after roundtrip; expected non-nil")
			}

			assertStreamItemsEqualJSON(t, tt.item, result)
		})
	}
}
