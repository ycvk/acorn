package runtime

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

func TestStreamProjectionRoundtrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		item StreamItem
	}{
		{
			name: "run_started",
			item: StreamItem{
				RunID: "run_rt", Sequence: 1, Kind: StreamKindRunStarted, CreatedAt: now,
				Payload: &RunStartedPayload{Input: "inspect the codebase"},
			},
		},
		{
			name: "run_completed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 2, Kind: StreamKindRunCompleted, CreatedAt: now,
				Payload: &RunCompletedPayload{
					Message: &StreamMessage{
						Role:      "assistant",
						Content:   "done",
						Reasoning: "thought process",
						ToolCalls: []StreamPlannedToolCall{
							{ID: "tc_1", Name: "read_file", ArgumentsJSON: `{"path":"README.md"}`},
						},
						Meta: map[string]any{"active_provider": "primary", "latency_ms": 150},
					},
				},
			},
		},
		{
			name: "run_failed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 3, Kind: StreamKindRunFailed, CreatedAt: now,
				Payload: &RunFailedPayload{Error: "model unavailable: connection refused"},
			},
		},
		{
			name: "run_interrupted",
			item: StreamItem{
				RunID: "run_rt", Sequence: 4, Kind: StreamKindRunInterrupted, CreatedAt: now,
				Payload: &RunInterruptedPayload{
					Interrupt: &StreamInterrupt{
						ContextCount: 2,
						Contexts: []StreamInterruptContext{
							{ID: "int_1", Address: "tool.run_command", Info: map[string]any{"kind": "approval", "cmd": "rm -rf /"}, IsRootCause: true},
							{ID: "int_2", Address: "tool.create_file", Info: map[string]any{"kind": "approval"}, IsRootCause: false},
						},
					},
				},
			},
		},
		{
			name: "run_resume_requested",
			item: StreamItem{
				RunID: "run_rt", Sequence: 5, Kind: StreamKindRunResumeRequested, CreatedAt: now,
				Payload: &RunResumeRequestedPayload{
					Targets: map[string]any{
						"interrupt_ids": []any{"int_1"},
						"approved":      true,
						"count":         1,
					},
				},
			},
		},
		{
			name: "decision_selected",
			item: StreamItem{
				RunID: "run_rt", Sequence: 7, Kind: StreamKindDecisionSelected, CreatedAt: now,
				Payload: &DecisionSelectedPayload{
					Action:              "skill",
					Intent:              "inspect the repo",
					SelectedSkillID:     "skill.inspect.repo",
					DecisionReason:      "profile_route",
					DecisionProfileHash: "abc123",
					ExplicitSkillID:     "skill.inspect.repo",
				},
			},
		},
		{
			name: "decision_blocked",
			item: StreamItem{
				RunID: "run_rt", Sequence: 8, Kind: StreamKindDecisionBlocked, CreatedAt: now,
				Payload: &DecisionBlockedPayload{
					Action:              "skill",
					Intent:              "deploy to prod",
					SelectedSkillID:     "",
					DecisionReason:      "missing_required_capability",
					DecisionProfileHash: "def456",
					ExplicitSkillID:     "",
				},
			},
		},

		{
			name: "skill_discovered",
			item: StreamItem{
				RunID: "run_rt", Sequence: 9, Kind: StreamKindSkillDiscovered, CreatedAt: now,
				Payload: &SkillDiscoveredPayload{Skill: &StreamSkill{
					SelectedID: "",
					Name:       "Inspect Repo",
					Source:     "workspace",
					Candidates: []StreamSkillCandidate{
						{ID: "skill.inspect.repo", Name: "Inspect Repo", Score: 145, MatchedTerms: []string{"inspect", "repo"}, Summary: "Quick repo overview", Requirements: StreamSkillRequirements{Tools: []string{"read_file", "run_command"}}},
					},
					NoSelectionReason: "ambiguous_top_score",
				}},
			},
		},
		{
			name: "skill_selected",
			item: StreamItem{
				RunID: "run_rt", Sequence: 10, Kind: StreamKindSkillSelected, CreatedAt: now,
				Payload: &SkillSelectedPayload{Skill: &StreamSkill{
					SelectedID:   "skill.inspect.repo",
					Name:         "Inspect Repo",
					Source:       "workspace",
					Path:         "/tmp/skills/inspect_repo",
					Instruction:  "Read README.md first.",
					Scripts:      []string{"scripts/quick_map.sh"},
					Requirements: StreamSkillRequirements{Tools: []string{"read_file", "run_command"}, Toolsets: []string{"fs"}, Bins: []string{"git"}, Env: []string{"HOME"}},
					Score:        145,
					MatchedTerms: []string{"inspect", "repo"},
				}},
			},
		},
		{
			name: "skill_loaded",
			item: StreamItem{
				RunID: "run_rt", Sequence: 11, Kind: StreamKindSkillLoaded, CreatedAt: now,
				Payload: &SkillLoadedPayload{Skill: &StreamSkill{
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
			item: StreamItem{
				RunID: "run_rt", Sequence: 12, Kind: StreamKindSkillFailed, CreatedAt: now,
				Payload: &SkillFailedPayload{Skill: &StreamSkill{
					SelectedID:    "skill.inspect.repo",
					Name:          "Inspect Repo",
					FailureReason: "missing_tool_use:read_file",
				}},
			},
		},
		{
			name: "skill_lifecycle",
			item: StreamItem{
				RunID: "run_rt", Sequence: 13, Kind: StreamKindSkillLifecycle, CreatedAt: now,
				Payload: &SkillLifecyclePayload{SkillLifecycle: &StreamSkillLifecycle{
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
			item: StreamItem{
				RunID: "run_rt", Sequence: 17, Kind: StreamKindContextCompressed, CreatedAt: now,
				Payload: &ContextCompressedPayload{ContextCompressed: &StreamContextCompressed{
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
			item: StreamItem{
				RunID: "run_rt", Sequence: 18, Kind: StreamKindContextPressure, CreatedAt: now,
				Payload: &ContextPressurePayload{ContextPressure: &StreamContextPressure{
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
			item: StreamItem{
				RunID: "run_rt", Sequence: 19, Kind: StreamKindAssistantMessage, CreatedAt: now,
				Payload: &AssistantMessagePayload{Message: &StreamMessage{
					Role:      "assistant",
					Content:   "I'll read the README for you.",
					Reasoning: "User wants repo overview",
					ToolCalls: []StreamPlannedToolCall{
						{ID: "tc_1", Name: "read_file", ArgumentsJSON: `{"path":"README.md"}`},
					},
					Meta: map[string]any{"active_provider": "primary"},
				}},
			},
		},
		{
			name: "tool_call_started",
			item: StreamItem{
				RunID: "run_rt", Sequence: 19, Kind: StreamKindToolCallStarted, CreatedAt: now,
				Payload: &ToolCallStartedPayload{ToolCall: &StreamToolCall{
					Provider:      "local",
					Name:          "read_file",
					ArgumentsJSON: `{"path":"README.md"}`,
				}},
			},
		},
		{
			name: "tool_call_succeeded",
			item: StreamItem{
				RunID: "run_rt", Sequence: 20, Kind: StreamKindToolCallSucceeded, CreatedAt: now,
				Payload: &ToolCallSucceededPayload{ToolCall: &StreamToolCall{
					Provider:   "local",
					Name:       "read_file",
					Output:     "# Acorn\n\nA Go-based agent runtime.",
					DurationMS: 120,
				}},
			},
		},
		{
			name: "tool_call_progress",
			item: StreamItem{
				RunID: "run_rt", Sequence: 21, Kind: StreamKindToolCallProgress, CreatedAt: now,
				Payload: &ToolCallProgressPayload{ToolCall: &StreamToolCallProgress{
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
			item: StreamItem{
				RunID: "run_rt", Sequence: 22, Kind: StreamKindToolCallFailed, CreatedAt: now,
				Payload: &ToolCallFailedPayload{ToolCall: &StreamToolCall{
					Provider:   "local",
					Name:       "run_command",
					Error:      "exit status 1: command not found",
					DurationMS: 50,
				}},
			},
		},
		{
			name: "tool_call_interrupted",
			item: StreamItem{
				RunID: "run_rt", Sequence: 23, Kind: StreamKindToolCallInterrupted, CreatedAt: now,
				Payload: &ToolCallInterruptedPayload{ToolCall: &StreamToolCall{
					Provider:          "local",
					Name:              "run_command",
					InterruptID:       "int_1",
					InterruptContexts: 1,
				}},
			},
		},
		{
			name: "provider.degraded",
			item: StreamItem{
				RunID: "run_rt", Sequence: 26, Kind: StreamKindProviderDegraded, CreatedAt: now,
				Payload: &ProviderDegradedPayload{
					AffectedProviders: []ProviderDegradedEntry{
						{Name: "openai", Transport: "https", Error: "rate limit"},
						{Name: "anthropic", Transport: "https"},
					},
				},
			},
		},

		{
			name: "mcp.tool_catalog_refreshed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 37, Kind: StreamKindMCPToolCatalogRefreshed, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
				},
			},
		},
		{
			name: "mcp.tool_catalog_refresh_failed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 38, Kind: StreamKindMCPToolCatalogRefreshFailed, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
					Error:        "catalog refresh timeout",
				},
			},
		},
		{
			name: "mcp.provider_added",
			item: StreamItem{
				RunID: "run_rt", Sequence: 39, Kind: StreamKindMCPProviderAdded, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "new_mcp",
					Transport:    "stdio",
				},
			},
		},
		{
			name: "mcp.provider_removed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 40, Kind: StreamKindMCPProviderRemoved, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "old_mcp",
					Transport:    "stdio",
				},
			},
		},
		{
			name: "mcp.provider_restarted",
			item: StreamItem{
				RunID: "run_rt", Sequence: 41, Kind: StreamKindMCPProviderRestarted, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
				},
			},
		},
		{
			name: "mcp.resource_catalog_refreshed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 42, Kind: StreamKindMCPResourceCatalogRefreshed, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
				},
			},
		},
		{
			name: "mcp.resource_catalog_refresh_failed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 43, Kind: StreamKindMCPResourceCatalogRefreshFailed, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
					Error:        "resource list timeout",
				},
			},
		},
		{
			name: "mcp.prompt_catalog_refreshed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 44, Kind: StreamKindMCPPromptCatalogRefreshed, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
				},
			},
		},
		{
			name: "mcp.prompt_catalog_refresh_failed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 45, Kind: StreamKindMCPPromptCatalogRefreshFailed, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
					Error:        "prompt list timeout",
				},
			},
		},
		{
			name: "mcp.auth_status_changed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 46, Kind: StreamKindMCPAuthStatusChanged, CreatedAt: now,
				Payload: &MCPProviderLifecyclePayload{
					ProviderName: "remote_mcp",
					Transport:    "stdio",
					AuthStatus:   "authenticated",
				},
			},
		},

		{
			name: "elicitation.pending",
			item: StreamItem{
				RunID: "run_rt", Sequence: 47, Kind: StreamKindElicitationPending, CreatedAt: now,
				Payload: &ElicitationPayload{
					ActionID: "act_1",
					Message:  "Please approve shell execution",
					RequestedSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"approved": map[string]any{"type": "boolean", "description": "Approve the action"},
						},
						"required": []any{"approved"},
					},
				},
			},
		},
		{
			name: "elicitation.decided",
			item: StreamItem{
				RunID: "run_rt", Sequence: 48, Kind: StreamKindElicitationDecided, CreatedAt: now,
				Payload: &ElicitationPayload{
					ActionID:        "act_1",
					Message:         "User approved",
					RequestedSchema: map[string]any{"type": "object"},
				},
			},
		},

		{
			name: "sampling.started",
			item: StreamItem{
				RunID: "run_rt", Sequence: 49, Kind: StreamKindSamplingStarted, CreatedAt: now,
				Payload: &SamplingPayload{
					RunID: "run_rt",
					Depth: 2,
					Model: "gpt-4o",
				},
			},
		},
		{
			name: "sampling.completed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 50, Kind: StreamKindSamplingCompleted, CreatedAt: now,
				Payload: &SamplingPayload{
					RunID: "run_rt",
					Depth: 2,
					Model: "gpt-4o",
				},
			},
		},
		{
			name: "sampling.failed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 51, Kind: StreamKindSamplingFailed, CreatedAt: now,
				Payload: &SamplingPayload{
					RunID: "run_rt",
					Depth: 3,
					Model: "claude-3",
				},
			},
		},

		{
			name: "plan_created",
			item: StreamItem{
				RunID: "run_rt", Sequence: 52, Kind: StreamKindPlanCreated, CreatedAt: now,
				Payload: &PlanCreatedPayload{
					Plan: &StreamPlan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps: []PlanStep{
							{ID: "s1", Action: "read the codebase", Status: "pending"},
							{ID: "s2", Action: "write tests", Status: "pending", DependsOn: []string{"s1"}},
							{ID: "s3", Action: "run tests", Status: "pending", DependsOn: []string{"s2"}},
						},
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
			},
		},
		{
			name: "plan_created_repo_aware_metadata",
			item: StreamItem{
				RunID: "run_rt", Sequence: 58, Kind: StreamKindPlanCreated, CreatedAt: now,
				Payload: &PlanCreatedPayload{
					Plan: &StreamPlan{
						PlanID:    "plan_repo_aware",
						SessionID: "sess_repo_aware",
						RunID:     "run_rt",
						Steps: []PlanStep{{
							ID:     "s1",
							Action: "update runtime plan metadata",
							Status: PlanStepPending,
							RepoTargets: []PlanRepoTarget{{
								Path:       "internal/runtime/plan_types.go",
								Symbol:     "PlanStep",
								StartLine:  30,
								EndLine:    44,
								Reason:     "plan metadata belongs on PlanStep",
								Confidence: "high",
							}},
							VerificationIntent: []VerificationIntent{{
								Kind:    "test",
								Command: []string{"go", "test", "./internal/runtime"},
								Paths:   []string{"internal/runtime"},
								Reason:  "runtime plan tests cover metadata",
							}},
							Risk:      PlanStepRiskWrite,
							ToolHints: []string{"read_file", "apply_unified_patch"},
						}},
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
			},
		},
		{
			name: "plan_updated",
			item: StreamItem{
				RunID: "run_rt", Sequence: 53, Kind: StreamKindPlanUpdated, CreatedAt: now,
				Payload: &PlanUpdatedPayload{
					Plan: &StreamPlan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps: []PlanStep{
							{ID: "s1", Action: "read the codebase", Status: "completed"},
							{ID: "s2", Action: "write tests", Status: "in_progress", DependsOn: []string{"s1"}},
							{ID: "s3", Action: "run tests", Status: "pending", DependsOn: []string{"s2"}},
						},
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
			},
		},
		{
			name: "plan_cleared",
			item: StreamItem{
				RunID: "run_rt", Sequence: 54, Kind: StreamKindPlanCleared, CreatedAt: now,
				Payload: &PlanClearedPayload{PlanID: "plan_1"},
			},
		},
		{
			name: "step_started",
			item: StreamItem{
				RunID: "run_rt", Sequence: 55, Kind: StreamKindStepStarted, CreatedAt: now,
				Payload: &PlanStepStartedPayload{PlanStepPayload: PlanStepPayload{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Plan: &StreamPlan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps:     []PlanStep{{ID: "s1", Action: "read the codebase", Status: "in_progress"}},
						CreatedAt: now,
						UpdatedAt: now,
					},
					Step:      &PlanStep{ID: "s1", Action: "read the codebase", Status: "in_progress"},
					UpdatedAt: now,
				}},
			},
		},
		{
			name: "step_completed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 56, Kind: StreamKindStepCompleted, CreatedAt: now,
				Payload: &PlanStepCompletedPayload{PlanStepPayload: PlanStepPayload{
					PlanID:    "plan_1",
					SessionID: "sess_1",
					RunID:     "run_rt",
					Plan: &StreamPlan{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Steps:     []PlanStep{{ID: "s1", Action: "read the codebase", Status: "completed"}},
						CreatedAt: now,
						UpdatedAt: now,
					},
					Step:      &PlanStep{ID: "s1", Action: "read the codebase", Status: "completed"},
					UpdatedAt: now,
				}},
			},
		},
		{
			name: "step_failed",
			item: StreamItem{
				RunID: "run_rt", Sequence: 57, Kind: StreamKindStepFailed, CreatedAt: now,
				Payload: &PlanStepFailedPayload{
					PlanStepPayload: PlanStepPayload{
						PlanID:    "plan_1",
						SessionID: "sess_1",
						RunID:     "run_rt",
						Plan: &StreamPlan{
							PlanID:    "plan_1",
							SessionID: "sess_1",
							RunID:     "run_rt",
							Steps:     []PlanStep{{ID: "s1", Action: "read the codebase", Status: "failed"}},
							CreatedAt: now,
							UpdatedAt: now,
						},
						Step:      &PlanStep{ID: "s1", Action: "read the codebase", Status: "failed"},
						UpdatedAt: now,
					},
					Error: "model returned no tool calls",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eventKind, payload, err := projectStreamItemToEvent(tt.item)
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

			result := projectEventToStreamItem(event)

			if result.Kind != tt.item.Kind {
				t.Fatalf("kind mismatch: got %q, want %q", result.Kind, tt.item.Kind)
			}

			if result.Payload == nil {
				t.Fatalf("payload is nil after roundtrip")
			}
			if got := result.Payload.StreamKind(); got != tt.item.Kind {
				t.Fatalf("payload stream kind mismatch: got %q, want %q", got, tt.item.Kind)
			}

			assertStreamItemsEqualJSON(t, tt.item, result)
		})
	}
}

func TestStreamProjectionRoundtrip_MCPKindsPreserved(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	mcpKinds := []StreamItemKind{
		StreamKindMCPToolCatalogRefreshed,
		StreamKindMCPToolCatalogRefreshFailed,
		StreamKindMCPProviderAdded,
		StreamKindMCPProviderRemoved,
		StreamKindMCPProviderRestarted,
		StreamKindMCPResourceCatalogRefreshed,
		StreamKindMCPResourceCatalogRefreshFailed,
		StreamKindMCPPromptCatalogRefreshed,
		StreamKindMCPPromptCatalogRefreshFailed,
		StreamKindMCPAuthStatusChanged,
	}

	sharedPayload := &MCPProviderLifecyclePayload{
		ProviderName: "shared_mcp",
		Transport:    "stdio",
		Error:        "test error",
		AuthStatus:   "expired",
	}

	for _, kind := range mcpKinds {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			item := StreamItem{
				RunID: "run_mcp_shared", Sequence: 1, Kind: kind, CreatedAt: now,
				Payload: sharedPayload,
			}

			eventKind, payload, err := projectStreamItemToEvent(item)
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

			result := projectEventToStreamItem(event)

			if result.Kind != kind {
				t.Fatalf("kind not preserved: got %q, want %q", result.Kind, kind)
			}

			p, ok := result.Payload.(*MCPProviderLifecyclePayload)
			if !ok {
				t.Fatalf("expected *MCPProviderLifecyclePayload, got %T", result.Payload)
			}
			if got := p.StreamKind(); got != kind {
				t.Fatalf("payload stream kind = %q, want %q", got, kind)
			}
			if p.ProviderName != "shared_mcp" {
				t.Fatalf("provider_name = %q, want shared_mcp", p.ProviderName)
			}
			if p.Error != "test error" {
				t.Fatalf("error = %q, want test error", p.Error)
			}
			if p.AuthStatus != "expired" {
				t.Fatalf("auth_status = %q, want expired", p.AuthStatus)
			}
		})
	}
}

func TestStreamProjectionRoundtrip_NilOptionalFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		item StreamItem
	}{
		{
			name: "run_completed_nil_message",
			item: StreamItem{
				RunID: "run_nil", Sequence: 1, Kind: StreamKindRunCompleted, CreatedAt: now,
				Payload: &RunCompletedPayload{Message: nil},
			},
		},
		{
			name: "run_interrupted_nil_interrupt",
			item: StreamItem{
				RunID: "run_nil", Sequence: 2, Kind: StreamKindRunInterrupted, CreatedAt: now,
				Payload: &RunInterruptedPayload{Interrupt: nil},
			},
		},
		{
			name: "skill_discovered_nil_skill",
			item: StreamItem{
				RunID: "run_nil", Sequence: 3, Kind: StreamKindSkillDiscovered, CreatedAt: now,
				Payload: &SkillDiscoveredPayload{Skill: nil},
			},
		},
		{
			name: "assistant_message_nil_message",
			item: StreamItem{
				RunID: "run_nil", Sequence: 4, Kind: StreamKindAssistantMessage, CreatedAt: now,
				Payload: &AssistantMessagePayload{Message: nil},
			},
		},
		{
			name: "tool_call_started_nil_toolcall",
			item: StreamItem{
				RunID: "run_nil", Sequence: 5, Kind: StreamKindToolCallStarted, CreatedAt: now,
				Payload: &ToolCallStartedPayload{ToolCall: nil},
			},
		},
		{
			name: "context_compressed_nil_context",
			item: StreamItem{
				RunID: "run_nil", Sequence: 7, Kind: StreamKindContextCompressed, CreatedAt: now,
				Payload: &ContextCompressedPayload{ContextCompressed: nil},
			},
		},
		{
			name: "elicitation_nil_schema",
			item: StreamItem{
				RunID: "run_nil", Sequence: 9, Kind: StreamKindElicitationPending, CreatedAt: now,
				Payload: &ElicitationPayload{ActionID: "act_nil", Message: "no schema"},
			},
		},
		{
			name: "run_resume_requested_nil_targets",
			item: StreamItem{
				RunID: "run_nil", Sequence: 10, Kind: StreamKindRunResumeRequested, CreatedAt: now,
				Payload: &RunResumeRequestedPayload{Targets: nil},
			},
		},
		{
			name: "provider_degraded_empty_providers",
			item: StreamItem{
				RunID: "run_nil", Sequence: 13, Kind: StreamKindProviderDegraded, CreatedAt: now,
				Payload: &ProviderDegradedPayload{AffectedProviders: nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eventKind, payload, err := projectStreamItemToEvent(tt.item)
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

			result := projectEventToStreamItem(event)

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
		item StreamItem
	}{
		{
			name: "tool_call_started_full",
			item: StreamItem{
				RunID: "run_tc", Sequence: 1, Kind: StreamKindToolCallStarted, CreatedAt: now,
				Payload: &ToolCallStartedPayload{ToolCall: &StreamToolCall{
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
			item: StreamItem{
				RunID: "run_tc", Sequence: 2, Kind: StreamKindToolCallSucceeded, CreatedAt: now,
				Payload: &ToolCallSucceededPayload{ToolCall: &StreamToolCall{
					Provider:   "mcp.remote",
					Name:       "read_file",
					Output:     "file contents here",
					DurationMS: 200,
				}},
			},
		},
		{
			name: "tool_call_progress_full",
			item: StreamItem{
				RunID: "run_tc", Sequence: 3, Kind: StreamKindToolCallProgress, CreatedAt: now,
				Payload: &ToolCallProgressPayload{ToolCall: &StreamToolCallProgress{
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
			item: StreamItem{
				RunID: "run_tc", Sequence: 4, Kind: StreamKindToolCallFailed, CreatedAt: now,
				Payload: &ToolCallFailedPayload{ToolCall: &StreamToolCall{
					Provider:   "local",
					Name:       "run_command",
					Error:      "exit status 127",
					DurationMS: 10,
				}},
			},
		},
		{
			name: "tool_call_interrupted_full",
			item: StreamItem{
				RunID: "run_tc", Sequence: 5, Kind: StreamKindToolCallInterrupted, CreatedAt: now,
				Payload: &ToolCallInterruptedPayload{ToolCall: &StreamToolCall{
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

			eventKind, payload, err := projectStreamItemToEvent(tt.item)
			if err != nil {
				t.Fatalf("forward projection failed: %v", err)
			}

			if m, ok := payload.(map[string]any); ok {
				if _, exists := m["tool_call"]; exists {
					t.Fatalf("tool_call key should have been removed from payload map, but it exists: %#v", m)
				}
			}

			event := events.EventRecord{
				Sequence:  tt.item.Sequence,
				RunID:     tt.item.RunID,
				Kind:      eventKind,
				CreatedAt: tt.item.CreatedAt,
				Payload:   payload,
			}

			result := projectEventToStreamItem(event)

			if result.GetToolCall() == nil && result.GetToolCallProgress() == nil {
				t.Fatalf("ToolCall is nil after roundtrip; expected non-nil")
			}

			assertStreamItemsEqualJSON(t, tt.item, result)
		})
	}
}

// assertStreamItemsEqualJSON compares two StreamItems by serializing both to JSON.
// This normalizes away Go-level type differences that are semantically equivalent
// in JSON (e.g., int 5 vs float64 5.0 inside any-typed fields).
func assertStreamItemsEqualJSON(t *testing.T, expected, actual StreamItem) {
	t.Helper()

	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected StreamItem: %v", err)
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("marshal actual StreamItem: %v", err)
	}

	if !reflect.DeepEqual(expectedJSON, actualJSON) {
		var expPretty, actPretty map[string]any
		if json.Unmarshal(expectedJSON, &expPretty) == nil && json.Unmarshal(actualJSON, &actPretty) == nil {
			expFormatted, _ := json.MarshalIndent(expPretty, "", "  ")
			actFormatted, _ := json.MarshalIndent(actPretty, "", "  ")
			t.Fatalf("roundtrip mismatch:\nexpected:\n%s\nactual:\n%s", expFormatted, actFormatted)
		}
		t.Fatalf("roundtrip mismatch:\nexpected: %s\nactual:   %s", expectedJSON, actualJSON)
	}
}
