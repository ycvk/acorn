package web

import (
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/workspace"
)

func TestRuntimeWorkbenchDTOFromDomainPreservesAggregatedSections(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	workbench := &app.RuntimeWorkbench{
		SessionID:       "session_42",
		Title:           "continue runtime work",
		State:           runtime.SessionStateInterrupted,
		LatestRunID:     "run_42",
		LatestRunStatus: events.RunStatusInterrupted,
		LatestRunMode:   "plan_execute",
		LatestRunDepth:  0,
		Resumable:       true,
		ResumeReason:    "pending actions remain",
		TraceSummary: &runtime.TraceSummary{
			ItemCount: 12,
			LastKind:  runtime.StreamKindSubagentCompleted,
		},
		LatestDecision: &decision.Record{
			RunID:               "run_42",
			Action:              decision.ActionResumeRun,
			SelectedSkillID:     "cs-feat-impl",
			DecisionReason:      "resume from interrupted plan step",
			DecisionProfileHash: "hash",
			CreatedAt:           now,
		},
		SessionSummary: &runtimehistory.SessionSummary{
			Summary:     "current runtime workbench state",
			RunStatus:   "interrupted",
			SourceRunID: "run_42",
			UpdatedAt:   now,
		},
		WorkspaceRoot: "/repo/acorn",
		GitStatus: app.WorkspaceGitStatus{
			WorkspaceRoot: "/repo/acorn",
			Available:     true,
			Branch:        "main",
			Clean:         false,
			Entries: []workspace.GitStatusEntry{{
				Path:           "frontend/src/components/workspace/workspace-inspector.tsx",
				IndexStatus:    "M",
				WorktreeStatus: "M",
			}},
		},
		MutationCheckpoints: []app.MutationCheckpointSummary{{
			CheckpointID:     "workspace_checkpoint_1",
			ToolResultRef:    "tool_result:run_42:call_1",
			ToolName:         "create_file",
			Status:           "succeeded",
			Paths:            []string{"tracked.txt"},
			Summary:          "checkpoint recorded",
			VerifiedDiffStat: "1 file changed",
			CreatedAt:        now,
		}},
		RollbackResults: []app.RollbackSummary{{
			RollbackID:    "workspace_rollback_1",
			CheckpointID:  "workspace_checkpoint_1",
			ToolResultRef: "tool_result:run_42:call_2",
			ToolName:      "rollback_workspace_checkpoint",
			Status:        "succeeded",
			RestoredPaths: []string{"tracked.txt"},
			Summary:       "rollback recorded",
			CreatedAt:     now,
		}},
		ContextEconomy: app.ContextEconomySummary{
			LatestPressure: &app.ContextPressureSummary{
				State:                 "warning",
				EstimatedInputTokens:  12000,
				EffectiveWindowTokens: 16000,
				PercentUsed:           75,
			},
			LatestCompression: &app.ContextCompressionSummary{
				BoundaryID:   "boundary_1",
				TokensBefore: 12000,
				TokensAfter:  5000,
				Summary:      "compressed earlier work",
			},
			ToolResultCount:         1,
			ElidedToolResultCount:   1,
			ToolResultTokenEstimate: 42,
			MemoryRefs:              []string{"facts/context-economy.md#tool-results"},
			ProcedureRefs:           []string{"skills/learned/context-economy.md#projection"},
			ToolResults: []app.ContextToolResultSummary{{
				ResultRef:     "tool_result:run_42:call_1",
				ToolName:      "read_file",
				Status:        "succeeded",
				Preview:       "preview",
				TokenEstimate: 42,
				FullTextBytes: 4096,
				Elided:        true,
				EvidenceRefs:  []string{"ev_1"},
			}},
		},
		ProviderUsage: app.ProviderUsageSummary{
			CallCount:        2,
			PromptTokens:     140,
			CompletionTokens: 30,
			TotalTokens:      170,
			CachedTokens:     65,
			ReasoningTokens:  6,
			Records: []app.ProviderUsageCallSummary{{
				UsageID:          "provider_usage:run_42:000001",
				CallSite:         "plan",
				ProviderName:     "openai",
				ModelName:        "gpt-test",
				PromptTokens:     100,
				CompletionTokens: 20,
				TotalTokens:      120,
				CachedTokens:     60,
				ReasoningTokens:  5,
				CreatedAt:        now,
			}},
		},
		Artifacts: []app.ArtifactSummary{{
			ArtifactID:          "artifact_report",
			RunID:               "run_42",
			SessionID:           "session_42",
			SourceToolResultRef: "tool_result:run_42:call_artifact",
			Kind:                "markdown",
			Title:               "Verification report",
			MIMEType:            "text/markdown",
			SizeBytes:           27,
			SHA256:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt:           now,
		}},
		TerminalSessions: []app.TerminalSessionSummary{{
			TerminalSessionID: "term_1",
			RunID:             "run_42",
			SessionID:         "session_42",
			Label:             "make test",
			CommandJSON:       `["make","test"]`,
			Cwd:               "/repo/acorn",
			Status:            "exited",
			ExitCode:          new(0),
			StdoutArtifactID:  "artifact_stdout",
			StartedAt:         &now,
			EndedAt:           &now,
			CreatedAt:         now,
			UpdatedAt:         now,
			Logs: []app.TerminalSessionLogSummary{{
				LogID:             "term_1_stdout",
				TerminalSessionID: "term_1",
				Stream:            "stdout",
				ArtifactID:        "artifact_stdout",
				StartOffset:       0,
				SizeBytes:         128,
				CreatedAt:         now,
			}},
		}},
		Plan: &runtime.Plan{
			PlanID:    "plan_1",
			SessionID: "session_42",
			RunID:     "run_42",
			Steps: []runtime.PlanStep{{
				ID:     "step_1",
				Action: "switch shell to workbench",
				Status: runtime.PlanStepInProgress,
				Evidence: []runtime.PlanEvidence{{
					ID:         "ev_1",
					StepID:     "step_1",
					Kind:       runtime.EvidenceKind("test"),
					Status:     runtime.EvidenceStatusPassed,
					Summary:    "go test ./internal/web",
					RecordedAt: now,
				}},
			}},
			CreatedAt: now,
			UpdatedAt: now,
		},
		Evidence: []runtime.PlanEvidence{{
			ID:         "ev_1",
			StepID:     "step_1",
			Kind:       runtime.EvidenceKind("test"),
			Status:     runtime.EvidenceStatusPassed,
			Summary:    "go test ./internal/web",
			RecordedAt: now,
		}},
		Subagents: []app.SubagentRun{{
			SubRunID:          "sub_1",
			ParentRunID:       "run_42",
			SessionID:         "session_42",
			Depth:             1,
			Task:              "verify workbench UI",
			ChildRunMode:      "fork",
			WorkspaceMode:     "worktree",
			WorktreePath:      "/tmp/worktree/run_child",
			ContextMessages:   3,
			OrchestrationMode: "single_agent",
			ParentStepID:      "s1",
			State:             "completed",
			FinalStatus:       "accepted",
			AcceptanceStatus:  "accepted",
			AcceptanceReasons: []string{"tests passed"},
			EvidenceRefs:      []string{"tool_result:run_child:call_1"},
			Summary:           "verified inspector sections",
			UpdatedAt:         now,
		}},
		NextStepHint: "可以继续当前工作，从上次中断处恢复。",
	}

	dto := runtimeWorkbenchDTOFromDomain(workbench)
	if dto.SessionID != "session_42" || dto.GitStatus.WorkspaceRoot != "/repo/acorn" {
		t.Fatalf("unexpected workbench dto: %+v", dto)
	}
	if !dto.GitStatus.Available || dto.GitStatus.Error != "" {
		t.Fatalf("unexpected git status dto: %+v", dto.GitStatus)
	}
	if dto.LatestRunMode != "plan_execute" || dto.LatestRunDepth != 0 {
		t.Fatalf("unexpected root run mode/depth: mode=%q depth=%d", dto.LatestRunMode, dto.LatestRunDepth)
	}
	if dto.Plan == nil || len(dto.Plan.Steps) != 1 {
		t.Fatalf("plan section missing: %+v", dto.Plan)
	}
	if len(dto.Evidence) != 1 || dto.Evidence[0].Summary != "go test ./internal/web" {
		t.Fatalf("evidence = %+v", dto.Evidence)
	}
	if len(dto.Subagents) != 1 || dto.Subagents[0].AcceptanceStatus != "accepted" {
		t.Fatalf("subagents = %+v", dto.Subagents)
	}
	if len(dto.MutationCheckpoints) != 1 || dto.MutationCheckpoints[0].CheckpointID != "workspace_checkpoint_1" {
		t.Fatalf("mutation checkpoints = %+v", dto.MutationCheckpoints)
	}
	if len(dto.RollbackResults) != 1 || dto.RollbackResults[0].RollbackID != "workspace_rollback_1" {
		t.Fatalf("rollback results = %+v", dto.RollbackResults)
	}
	if dto.ContextEconomy.ToolResultCount != 1 || dto.ContextEconomy.ElidedToolResultCount != 1 || dto.ContextEconomy.ToolResultTokenEstimate != 42 {
		t.Fatalf("context economy counts = %+v", dto.ContextEconomy)
	}
	if dto.ContextEconomy.LatestPressure == nil || dto.ContextEconomy.LatestPressure.State != "warning" {
		t.Fatalf("latest pressure = %+v", dto.ContextEconomy.LatestPressure)
	}
	if dto.ContextEconomy.LatestCompression == nil || dto.ContextEconomy.LatestCompression.BoundaryID != "boundary_1" {
		t.Fatalf("latest compression = %+v", dto.ContextEconomy.LatestCompression)
	}
	if len(dto.ContextEconomy.ToolResults) != 1 || dto.ContextEconomy.ToolResults[0].ResultRef != "tool_result:run_42:call_1" || !dto.ContextEconomy.ToolResults[0].Elided {
		t.Fatalf("tool result economy = %+v", dto.ContextEconomy.ToolResults)
	}
	if len(dto.ContextEconomy.MemoryRefs) != 1 || len(dto.ContextEconomy.ProcedureRefs) != 1 {
		t.Fatalf("context refs = memory:%+v procedure:%+v", dto.ContextEconomy.MemoryRefs, dto.ContextEconomy.ProcedureRefs)
	}
	if dto.ProviderUsage.CallCount != 2 || dto.ProviderUsage.TotalTokens != 170 || dto.ProviderUsage.CachedTokens != 65 || dto.ProviderUsage.ReasoningTokens != 6 {
		t.Fatalf("provider usage = %+v", dto.ProviderUsage)
	}
	if len(dto.ProviderUsage.Records) != 1 || dto.ProviderUsage.Records[0].UsageID != "provider_usage:run_42:000001" {
		t.Fatalf("provider usage records = %+v", dto.ProviderUsage.Records)
	}
	if len(dto.Artifacts) != 1 || dto.Artifacts[0].ArtifactID != "artifact_report" || dto.Artifacts[0].SourceToolResultRef != "tool_result:run_42:call_artifact" {
		t.Fatalf("artifacts = %+v", dto.Artifacts)
	}
	if len(dto.TerminalSessions) != 1 || dto.TerminalSessions[0].TerminalSessionID != "term_1" || len(dto.TerminalSessions[0].Logs) != 1 {
		t.Fatalf("terminal sessions = %+v", dto.TerminalSessions)
	}
	if dto.Subagents[0].OrchestrationMode != "single_agent" || dto.Subagents[0].ParentStepID != "s1" {
		t.Fatalf("subagent truth = %+v", dto.Subagents[0])
	}
	if dto.Subagents[0].ChildRunMode != "fork" || dto.Subagents[0].WorkspaceMode != "worktree" || dto.Subagents[0].ContextMessages != 3 || len(dto.Subagents[0].EvidenceRefs) != 1 {
		t.Fatalf("subagent lineage truth = %+v", dto.Subagents[0])
	}
	if dto.NextStepHint != "可以继续当前工作，从上次中断处恢复。" {
		t.Fatalf("NextStepHint = %q", dto.NextStepHint)
	}
}
