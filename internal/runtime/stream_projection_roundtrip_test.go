package runtime

import (
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
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
			name: "context_compressed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 17, Kind: stream.StreamKindContextCompressed, CreatedAt: now,
				Payload: map[string]any{"context_compressed": &stream.StreamContextCompressed{
					BoundaryID:     "ctxb_run_rt_0001",
					FirstIndex:     2,
					LastIndex:      8,
					TokensBefore:   12000,
					TokensAfter:    4000,
					SummarySnippet: "The user asked to inspect the repo...",
				}},
			},
		},
		{
			name: "context_pressure",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 18, Kind: stream.StreamKindContextPressure, CreatedAt: now,
				Payload: map[string]any{"context_pressure": &stream.StreamContextPressure{
					State:                      "auto_compact",
					EstimatedInputTokens:       12000,
					EffectiveWindowTokens:      14000,
					WarningThresholdTokens:     10000,
					AutoCompactThresholdTokens: 11000,
					BlockingThresholdTokens:    13000,
					PercentUsed:                85,
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
			name: "tool_call_progress",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 21, Kind: stream.StreamKindToolCallProgress, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCallProgress{
					Provider:      "local",
					Name:          "run_command",
					CallID:        "call_1",
					ArgumentsJSON: `{"command":"go test ./internal/runtime"}`,
					Delta:         "ok github.com/ycvk/acorn/internal/runtime",
					Sequence:      1,
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
			name: "provider.degraded",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 26, Kind: stream.StreamKindProviderDegraded, CreatedAt: now,
				Payload: map[string]any{"affected_providers": []map[string]any{
					{"name": "openai", "transport": "https", "error": "rate limit"},
					{"name": "anthropic", "transport": "https"},
				}},
			},
		},

		{
			name: "mcp.tool_catalog_refreshed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 37, Kind: stream.StreamKindMCPToolCatalogRefreshed, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio"},
			},
		},
		{
			name: "mcp.tool_catalog_refresh_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 38, Kind: stream.StreamKindMCPToolCatalogRefreshFailed, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio", "error": "catalog refresh timeout"},
			},
		},
		{
			name: "mcp.provider_added",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 39, Kind: stream.StreamKindMCPProviderAdded, CreatedAt: now,
				Payload: map[string]any{"provider_name": "new_mcp", "transport": "stdio"},
			},
		},
		{
			name: "mcp.provider_removed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 40, Kind: stream.StreamKindMCPProviderRemoved, CreatedAt: now,
				Payload: map[string]any{"provider_name": "old_mcp", "transport": "stdio"},
			},
		},
		{
			name: "mcp.provider_restarted",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 41, Kind: stream.StreamKindMCPProviderRestarted, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio"},
			},
		},
		{
			name: "mcp.resource_catalog_refreshed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 42, Kind: stream.StreamKindMCPResourceCatalogRefreshed, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio"},
			},
		},
		{
			name: "mcp.resource_catalog_refresh_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 43, Kind: stream.StreamKindMCPResourceCatalogRefreshFailed, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio", "error": "resource list timeout"},
			},
		},
		{
			name: "mcp.prompt_catalog_refreshed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 44, Kind: stream.StreamKindMCPPromptCatalogRefreshed, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio"},
			},
		},
		{
			name: "mcp.prompt_catalog_refresh_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 45, Kind: stream.StreamKindMCPPromptCatalogRefreshFailed, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio", "error": "prompt list timeout"},
			},
		},
		{
			name: "mcp.auth_status_changed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 46, Kind: stream.StreamKindMCPAuthStatusChanged, CreatedAt: now,
				Payload: map[string]any{"provider_name": "remote_mcp", "transport": "stdio", "auth_status": "authenticated"},
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

		{
			name: "sampling.started",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 49, Kind: stream.StreamKindSamplingStarted, CreatedAt: now,
				Payload: map[string]any{"run_id": "run_rt", "depth": 2, "model": "gpt-4o"},
			},
		},
		{
			name: "sampling.completed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 50, Kind: stream.StreamKindSamplingCompleted, CreatedAt: now,
				Payload: map[string]any{"run_id": "run_rt", "depth": 2, "model": "gpt-4o"},
			},
		},
		{
			name: "sampling.failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 51, Kind: stream.StreamKindSamplingFailed, CreatedAt: now,
				Payload: map[string]any{"run_id": "run_rt", "depth": 3, "model": "claude-3"},
			},
		},

		{
			name: "plan_created",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 52, Kind: stream.StreamKindPlanCreated, CreatedAt: now,
				Payload: stream.PlanStepPayloadToMap(stream.PlanStepPayload{Plan: &model.Plan{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Steps: []model.PlanStep{
						{ID: "s1", Action: "read the codebase", Status: "pending"},
						{ID: "s2", Action: "write tests", Status: "pending", DependsOn: []string{"s1"}},
						{ID: "s3", Action: "run tests", Status: "pending", DependsOn: []string{"s2"}},
					},
					CreatedAt: now,
					UpdatedAt: now,
				}}),
			},
		},
		{
			name: "plan_created_repo_aware_metadata",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 58, Kind: stream.StreamKindPlanCreated, CreatedAt: now,
				Payload: stream.PlanStepPayloadToMap(stream.PlanStepPayload{Plan: &model.Plan{
					PlanID:    "plan_repo_aware",
					SessionID: "sess_repo_aware",
					RunID:     "run_rt",
					Steps: []model.PlanStep{{
						ID:     "s1",
						Action: "update runtime plan metadata",
						Status: model.PlanStepPending,
						RepoTargets: []model.PlanRepoTarget{{
							Path:       "internal/model/plan.go",
							Symbol:     "model.PlanStep",
							StartLine:  30,
							EndLine:    44,
							Reason:     "plan metadata belongs on model.PlanStep",
							Confidence: "high",
						}},
						VerificationIntent: []model.VerificationIntent{{
							Kind:    "test",
							Command: []string{"go", "test", "./internal/runtime"},
							Paths:   []string{"internal/runtime"},
							Reason:  "runtime plan tests cover metadata",
						}},
						Risk:      model.PlanStepRiskWrite,
						ToolHints: []string{"read_file", "apply_unified_patch"},
					}},
					CreatedAt: now,
					UpdatedAt: now,
				}}),
			},
		},
		{
			name: "plan_updated",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 53, Kind: stream.StreamKindPlanUpdated, CreatedAt: now,
				Payload: stream.PlanStepPayloadToMap(stream.PlanStepPayload{Plan: &model.Plan{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Steps: []model.PlanStep{
						{ID: "s1", Action: "read the codebase", Status: "completed"},
						{ID: "s2", Action: "write tests", Status: "in_progress", DependsOn: []string{"s1"}},
						{ID: "s3", Action: "run tests", Status: "pending", DependsOn: []string{"s2"}},
					},
					CreatedAt: now,
					UpdatedAt: now,
				}}),
			},
		},
		{
			name: "plan_updated",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 54, Kind: stream.StreamKindPlanUpdated, CreatedAt: now,
				Payload: map[string]any{"plan_id": "plan_1"},
			},
		},
		{
			name: "step_started",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 55, Kind: stream.StreamKindStepStarted, CreatedAt: now,
				Payload: stream.PlanStepPayloadToMap(stream.PlanStepPayload{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Plan: &model.Plan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps:     []model.PlanStep{{ID: "s1", Action: "read the codebase", Status: "in_progress"}},
						CreatedAt: now,
						UpdatedAt: now,
					},
					Step:      &model.PlanStep{ID: "s1", Action: "read the codebase", Status: "in_progress"},
					UpdatedAt: now,
				}),
			},
		},
		{
			name: "step_completed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 56, Kind: stream.StreamKindStepCompleted, CreatedAt: now,
				Payload: stream.PlanStepPayloadToMap(stream.PlanStepPayload{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Plan: &model.Plan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps:     []model.PlanStep{{ID: "s1", Action: "read the codebase", Status: "completed"}},
						CreatedAt: now,
						UpdatedAt: now,
					},
					Step:      &model.PlanStep{ID: "s1", Action: "read the codebase", Status: "completed"},
					UpdatedAt: now,
				}),
			},
		},
		{
			name: "step_failed",
			item: stream.StreamItem{
				RunID: "run_rt", Sequence: 57, Kind: stream.StreamKindStepFailed, CreatedAt: now,
				Payload: stream.PlanStepPayloadToMap(stream.PlanStepPayload{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Plan: &model.Plan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps:     []model.PlanStep{{ID: "s1", Action: "read the codebase", Status: "failed"}},
						CreatedAt: now,
						UpdatedAt: now,
					},
					Step:      &model.PlanStep{ID: "s1", Action: "read the codebase", Status: "failed"},
					UpdatedAt: now,
				}),
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

func TestStreamProjectionRoundtrip_MCPKindsPreserved(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	sharedPayload := map[string]any{
		"provider_name": "shared_mcp",
		"transport":     "stdio",
		"error":         "test error",
		"auth_status":   "expired",
	}

	mcpKinds := []stream.StreamItemKind{
		stream.StreamKindMCPToolCatalogRefreshed,
		stream.StreamKindMCPToolCatalogRefreshFailed,
		stream.StreamKindMCPProviderAdded,
		stream.StreamKindMCPProviderRemoved,
		stream.StreamKindMCPProviderRestarted,
		stream.StreamKindMCPResourceCatalogRefreshed,
		stream.StreamKindMCPResourceCatalogRefreshFailed,
		stream.StreamKindMCPPromptCatalogRefreshed,
		stream.StreamKindMCPPromptCatalogRefreshFailed,
		stream.StreamKindMCPAuthStatusChanged,
	}

	for _, kind := range mcpKinds {
		t.Run(string(kind), func(t *testing.T) {
			item := stream.StreamItem{
				RunID: "run_mcp_shared", Sequence: 1, Kind: kind, CreatedAt: now,
				Payload: sharedPayload,
			}
			eventKind, payload, err := stream.ProjectStreamItemToEvent(item)
			if err != nil {
				t.Fatalf("forward projection failed: %v", err)
			}
			event := events.EventRecord{
				Sequence:  item.Sequence,
				RunID:     item.RunID,
				Kind:      eventKind,
				CreatedAt: item.CreatedAt,
				Payload:   payload,
			}
			result := mustProjectEventToStreamItem(t, event)
			if result.Kind != kind {
				t.Fatalf("kind not preserved: got %q, want %q", result.Kind, kind)
			}
			p := result.Payload
			if p["provider_name"] != "shared_mcp" {
				t.Fatalf("provider_name = %q, want shared_mcp", p["provider_name"])
			}
			if p["error"] != "test error" {
				t.Fatalf("error = %q, want test error", p["error"])
			}
			if p["auth_status"] != "expired" {
				t.Fatalf("auth_status = %q, want expired", p["auth_status"])
			}
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
			name: "context_compressed_nil_context",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 7, Kind: stream.StreamKindContextCompressed, CreatedAt: now,
				Payload: map[string]any{"context_compressed": nil},
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
		{
			name: "provider_degraded_empty_providers",
			item: stream.StreamItem{
				RunID: "run_nil", Sequence: 13, Kind: stream.StreamKindProviderDegraded, CreatedAt: now,
				Payload: map[string]any{"affected_providers": nil},
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
			name: "tool_call_progress_full",
			item: stream.StreamItem{
				RunID: "run_tc", Sequence: 3, Kind: stream.StreamKindToolCallProgress, CreatedAt: now,
				Payload: map[string]any{"tool_call": &stream.StreamToolCallProgress{
					Provider:      "local",
					Name:          "run_command",
					CallID:        "call_1",
					ArgumentsJSON: `{"command":"make test"}`,
					Delta:         "running package internal/runtime",
					Sequence:      7,
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

			if result.GetToolCall() == nil && result.GetToolCallProgress() == nil {
				t.Fatalf("ToolCall is nil after roundtrip; expected non-nil")
			}

			assertStreamItemsEqualJSON(t, tt.item, result)
		})
	}
}
