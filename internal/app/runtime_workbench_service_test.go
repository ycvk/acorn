package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/providers"
	"github.com/ycvk/acorn/internal/store"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/workspace"
)

func TestRuntimeWorkbenchServiceLoadAggregatesRuntimeTruth(t *testing.T) {
	root := t.TempDir()
	initGitRepoForWorkbenchTest(t, root)
	if err := os.WriteFile(root+"/tracked.txt", []byte("seed"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitForWorkbenchTest(t, root, "add", "tracked.txt")
	runGitForWorkbenchTest(t, root, "commit", "-m", "seed")
	if err := os.WriteFile(root+"/tracked.txt", []byte("changed"), 0o644); err != nil {
		t.Fatalf("mutate tracked file: %v", err)
	}

	ws, err := workspace.New(workspace.Config{RootDir: root})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	store := &runtimeWorkbenchStoreStub{
		session: &events.SessionRecord{
			SessionID: "session_1",
			Title:     "Runtime Workbench",
		},
		run: &events.RunRecord{
			RunID:             "run_1",
			SessionID:         "session_1",
			Status:            events.RunStatusSucceeded,
			CheckpointID:      "checkpoint_1",
			OrchestrationMode: "plan_execute",
			Depth:             0,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		eventsByRun: map[string][]events.EventRecord{
			"run_1": {
				{
					RunID:     "run_1",
					Kind:      string(stream.StreamKindSubagentStarted),
					Sequence:  1,
					CreatedAt: now,
					Payload: map[string]any{
						"sub_run_id":         "sub_1",
						"parent_id":          "run_1",
						"session_id":         "session_1",
						"depth":              1,
						"task":               "verify ui",
						"child_run_mode":     "fork",
						"workspace_mode":     "worktree",
						"worktree_path":      "/tmp/worktree/run_child",
						"context_messages":   3,
						"orchestration_mode": "single_agent",
						"parent_step_id":     "s1",
					},
				},
				{
					RunID:     "run_1",
					Kind:      string(stream.StreamKindSubagentCompleted),
					Sequence:  2,
					CreatedAt: now.Add(time.Second),
					Payload: map[string]any{
						"sub_run_id":         "sub_1",
						"parent_id":          "run_1",
						"session_id":         "session_1",
						"summary":            "done",
						"final_status":       "accepted",
						"acceptance_status":  "accepted",
						"acceptance_reasons": []string{"tests passed"},
						"child_run_mode":     "fork",
						"workspace_mode":     "worktree",
						"worktree_path":      "/tmp/worktree/run_child",
						"evidence_refs":      []string{"tool_result:run_child:call_1"},
						"orchestration_mode": "single_agent",
						"parent_step_id":     "s1",
					},
				},
				{
					RunID:     "run_1",
					Kind:      string(stream.StreamKindStepCompleted),
					Sequence:  3,
					CreatedAt: now.Add(2 * time.Second),
					Payload:   map[string]any{},
				},
				{
					RunID:     "run_1",
					Kind:      "context.pressure",
					Sequence:  4,
					CreatedAt: now.Add(3 * time.Second),
					Payload: map[string]any{"context_pressure": &stream.StreamContextPressure{
						State:                 "warning",
						EstimatedInputTokens:  12000,
						EffectiveWindowTokens: 16000,
						PercentUsed:           75,
					}},
				},
				{
					RunID:     "run_1",
					Kind:      "context.compressed",
					Sequence:  5,
					CreatedAt: now.Add(4 * time.Second),
					Payload: map[string]any{"context_compressed": &stream.StreamContextCompressed{
						BoundaryID:     "boundary_1",
						TokensBefore:   12000,
						TokensAfter:    5000,
						SummarySnippet: "compressed earlier work",
					}},
				},
				{
					RunID:     "run_1",
					Kind:      "memory.prepared",
					Sequence:  6,
					CreatedAt: now.Add(5 * time.Second),
					Payload: map[string]any{"memory_prepared": &stream.StreamMemoryPrepared{
						Entries: []stream.StreamMemoryPreparedEntry{{
							Ref:   "facts/context-economy.md#tool-results",
							Kind:  "fact",
							Title: "Tool result economy",
						}},
					}},
				},
				{
					RunID:     "run_1",
					Kind:      "procedure.activation",
					Sequence:  7,
					CreatedAt: now.Add(6 * time.Second),
					Payload: map[string]any{"procedure_activation": &stream.StreamProcedureActivation{
						ProcedureRef: "skills/learned/context-economy.md#projection",
						Phase:        "injected",
					}},
				},
			},
		},
		decision: &decision.Record{
			RunID:     "run_1",
			SessionID: "session_1",
			Action:    decision.ActionResumeRun,
			CreatedAt: now,
		},
		plan: &model.Plan{
			PlanID:    "plan_1",
			SessionID: "session_1",
			RunID:     "run_1",
			Steps: []model.PlanStep{{
				ID:     "step_1",
				Action: "inspect workbench",
				Status: model.PlanStepCompleted,
				Evidence: []model.PlanEvidence{{
					ID:         "ev_1",
					StepID:     "step_1",
					Kind:       model.EvidenceKindTest,
					Status:     model.EvidenceStatusPassed,
					Summary:    "go test ./internal/app",
					RecordedAt: now,
				}},
			}},
			CreatedAt: now,
			UpdatedAt: now,
		},
		toolResultsByRun: map[string][]store.ToolResultRecord{
			"run_1": {
				{
					ResultRef:     "tool_result:run_1:call_create",
					RunID:         "run_1",
					SessionID:     "session_1",
					TurnIndex:     2,
					CallID:        "call_create",
					ToolName:      "create_file",
					ArgumentsJSON: `{"path":"tracked.txt","content":"changed"}`,
					Status:        store.ToolResultStatusSucceeded,
					Preview:       `{"path":"tracked.txt","message":"ok","checkpoint_id":"workspace_checkpoint_1","checkpoint_paths":["tracked.txt"],"verified_bytes":7,"verified_content":"changed","verification_truncated":false}`,
					FullText:      `{"path":"tracked.txt","message":"ok","checkpoint_id":"workspace_checkpoint_1","checkpoint_paths":["tracked.txt"],"verified_bytes":7,"verified_content":"changed","verification_truncated":false,"extra":"large result body"}`,
					TokenEstimate: 42,
					SideEffects: []store.SideEffectRef{{
						Kind: workspace.MutationCheckpointEffect,
						Ref:  "workspace_checkpoint_1",
						Path: "tracked.txt",
					}},
					EvidenceRefs: []store.EvidenceRef{{
						Kind: "plan_evidence",
						Ref:  "ev_1",
					}},
					CreatedAt: now,
				},
				{
					ResultRef:     "tool_result:run_1:call_rollback",
					RunID:         "run_1",
					SessionID:     "session_1",
					TurnIndex:     3,
					CallID:        "call_rollback",
					ToolName:      "rollback_workspace_checkpoint",
					ArgumentsJSON: `{"checkpoint_id":"workspace_checkpoint_1"}`,
					Status:        store.ToolResultStatusSucceeded,
					Preview:       `{"checkpoint_id":"workspace_checkpoint_1","rollback_id":"workspace_rollback_1","status":"succeeded","restored_paths":["tracked.txt"],"conflict_paths":[],"error":""}`,
					FullText:      `{"checkpoint_id":"workspace_checkpoint_1","rollback_id":"workspace_rollback_1","status":"succeeded","restored_paths":["tracked.txt"],"conflict_paths":[],"error":""}`,
					TokenEstimate: 21,
					SideEffects: []store.SideEffectRef{{
						Kind: workspace.MutationRollbackEffect,
						Ref:  "workspace_rollback_1",
						Path: "tracked.txt",
					}},
					CreatedAt: now.Add(3 * time.Second),
				},
			},
		},
		artifactsByRun: map[string][]store.ArtifactRecord{
			"run_1": {
				{
					ArtifactID:          "artifact_report",
					RunID:               "run_1",
					SessionID:           "session_1",
					SourceToolResultRef: "tool_result:run_1:call_artifact",
					Kind:                store.ArtifactKindMarkdown,
					Title:               "Verification report",
					MIMEType:            "text/markdown",
					SizeBytes:           27,
					SHA256:              strings.Repeat("a", 64),
					CreatedAt:           now.Add(2 * time.Second),
				},
			},
		},
		providerUsagesByRun: map[string][]providers.UsageRecord{
			"run_1": {
				{
					UsageID:          "provider_usage:run_1:000001",
					RunID:            "run_1",
					SessionID:        "session_1",
					CallSite:         providers.CallSitePlan,
					ProviderName:     "openai",
					ModelName:        "gpt-test",
					PromptTokens:     100,
					CompletionTokens: 20,
					TotalTokens:      120,
					CachedTokens:     60,
					ReasoningTokens:  5,
					CreatedAt:        now,
				},
				{
					UsageID:          "provider_usage:run_1:000002",
					RunID:            "run_1",
					SessionID:        "session_1",
					CallSite:         providers.CallSiteAct,
					ProviderName:     "openai",
					ModelName:        "gpt-test",
					PromptTokens:     40,
					CompletionTokens: 10,
					TotalTokens:      50,
					CachedTokens:     5,
					ReasoningTokens:  1,
					CreatedAt:        now.Add(time.Second),
				},
			},
		},
		summary: &model.SessionSummary{
			SessionID:   "session_1",
			SourceRunID: "run_1",
			RunStatus:   "interrupted",
			Summary:     "session summary",
			UpdatedAt:   now,
		},
	}

	service := NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{Workspace: ws}, store, nil)
	workbench, err := service.Load(context.Background(), "session_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if workbench.SessionID != "session_1" || workbench.Plan == nil {
		t.Fatalf("unexpected workbench: %+v", workbench)
	}
	if !workbench.GitStatus.Available || workbench.GitStatus.Error != "" {
		t.Fatalf("expected available git status, got %+v", workbench.GitStatus)
	}
	if workbench.LatestRunMode != "plan_execute" || workbench.LatestRunDepth != 0 {
		t.Fatalf("unexpected root lineage fields: mode=%q depth=%d", workbench.LatestRunMode, workbench.LatestRunDepth)
	}
	if len(workbench.Subagents) != 1 || workbench.Subagents[0].AcceptanceStatus != "accepted" {
		t.Fatalf("unexpected subagents: %+v", workbench.Subagents)
	}
	if workbench.Subagents[0].OrchestrationMode != "single_agent" || workbench.Subagents[0].ParentStepID != "s1" {
		t.Fatalf("unexpected subagent mode/step truth: %+v", workbench.Subagents[0])
	}
	if workbench.Subagents[0].ChildRunMode != "fork" || workbench.Subagents[0].WorkspaceMode != "worktree" || workbench.Subagents[0].ContextMessages != 3 || len(workbench.Subagents[0].EvidenceRefs) != 1 {
		t.Fatalf("unexpected subagent lineage truth: %+v", workbench.Subagents[0])
	}
	if workbench.NextStepHint == "" {
		t.Fatal("expected next step hint")
	}
	if len(workbench.MutationCheckpoints) != 1 {
		t.Fatalf("mutation checkpoints = %+v", workbench.MutationCheckpoints)
	}
	if got := workbench.MutationCheckpoints[0]; got.CheckpointID != "workspace_checkpoint_1" || len(got.Paths) != 1 || got.Paths[0] != "tracked.txt" || got.ToolName != "create_file" {
		t.Fatalf("unexpected checkpoint summary: %+v", got)
	}
	if len(workbench.RollbackResults) != 1 {
		t.Fatalf("rollback results = %+v", workbench.RollbackResults)
	}
	if got := workbench.RollbackResults[0]; got.RollbackID != "workspace_rollback_1" || got.CheckpointID != "workspace_checkpoint_1" || len(got.RestoredPaths) != 1 || got.RestoredPaths[0] != "tracked.txt" {
		t.Fatalf("unexpected rollback summary: %+v", got)
	}
	if workbench.ContextEconomy.ToolResultCount != 2 || workbench.ContextEconomy.ElidedToolResultCount != 1 {
		t.Fatalf("unexpected context economy counts: %+v", workbench.ContextEconomy)
	}
	if workbench.ContextEconomy.ToolResultTokenEstimate != 63 {
		t.Fatalf("tool result token estimate = %d", workbench.ContextEconomy.ToolResultTokenEstimate)
	}
	if workbench.ContextEconomy.LatestPressure == nil || workbench.ContextEconomy.LatestPressure.State != "warning" || workbench.ContextEconomy.LatestPressure.EstimatedInputTokens != 12000 {
		t.Fatalf("unexpected latest pressure: %+v", workbench.ContextEconomy.LatestPressure)
	}
	if workbench.ContextEconomy.LatestCompression == nil || workbench.ContextEconomy.LatestCompression.BoundaryID != "boundary_1" {
		t.Fatalf("unexpected latest compression: %+v", workbench.ContextEconomy.LatestCompression)
	}
	if len(workbench.ContextEconomy.MemoryRefs) != 1 || workbench.ContextEconomy.MemoryRefs[0] != "facts/context-economy.md#tool-results" {
		t.Fatalf("unexpected memory refs: %+v", workbench.ContextEconomy.MemoryRefs)
	}
	if len(workbench.ContextEconomy.ProcedureRefs) != 1 || workbench.ContextEconomy.ProcedureRefs[0] != "skills/learned/context-economy.md#projection" {
		t.Fatalf("unexpected procedure refs: %+v", workbench.ContextEconomy.ProcedureRefs)
	}
	if len(workbench.ContextEconomy.ToolResults) != 2 || !workbench.ContextEconomy.ToolResults[0].Elided || len(workbench.ContextEconomy.ToolResults[0].EvidenceRefs) != 1 {
		t.Fatalf("unexpected tool result economy: %+v", workbench.ContextEconomy.ToolResults)
	}
	if len(workbench.Artifacts) != 1 || workbench.Artifacts[0].ArtifactID != "artifact_report" || workbench.Artifacts[0].SourceToolResultRef != "tool_result:run_1:call_artifact" {
		t.Fatalf("unexpected artifacts: %+v", workbench.Artifacts)
	}
	if workbench.ProviderUsage.CallCount != 2 || workbench.ProviderUsage.TotalTokens != 170 || workbench.ProviderUsage.CachedTokens != 65 || workbench.ProviderUsage.ReasoningTokens != 6 {
		t.Fatalf("unexpected provider usage summary: %+v", workbench.ProviderUsage)
	}
	if len(workbench.ProviderUsage.Records) != 2 || workbench.ProviderUsage.Records[0].CallSite != providers.CallSitePlan {
		t.Fatalf("unexpected provider usage records: %+v", workbench.ProviderUsage.Records)
	}
}

func TestRuntimeWorkbenchServiceLoadReportsGitUnavailable(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(workspace.Config{RootDir: root})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	store := &runtimeWorkbenchStoreStub{
		session: &events.SessionRecord{
			SessionID: "session_1",
			Title:     "No Git Workspace",
		},
	}

	service := NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{Workspace: ws}, store, nil)
	workbench, err := service.Load(context.Background(), "session_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if workbench.GitStatus.Available {
		t.Fatalf("git status should be unavailable: %+v", workbench.GitStatus)
	}
	if workbench.GitStatus.WorkspaceRoot != root {
		t.Fatalf("workspace root = %q, want %q", workbench.GitStatus.WorkspaceRoot, root)
	}
	if !strings.Contains(workbench.GitStatus.Error, "not a git repository") {
		t.Fatalf("git unavailable error = %q", workbench.GitStatus.Error)
	}
}

func TestRuntimeWorkbenchServiceLoadFailsLoudOnProjectionError(t *testing.T) {
	store := &runtimeWorkbenchStoreStub{
		session:       &events.SessionRecord{SessionID: "session_1", Title: "Runtime Workbench"},
		run:           &events.RunRecord{RunID: "run_1", SessionID: "session_1", Status: events.RunStatusSucceeded},
		summary:       &model.SessionSummary{SessionID: "session_1", Summary: "ok"},
		loadEventsErr: errors.New("load events boom"),
	}

	service := NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{}, store, nil)
	_, err := service.Load(context.Background(), "session_1")
	if err == nil || !strings.Contains(err.Error(), "load events for run run_1: load events boom") {
		t.Fatalf("expected fail-loud events error, got %v", err)
	}
}

func TestParseRollbackSummaryRecordPreservesFailedNonJSONToolResult(t *testing.T) {
	now := time.Now().UTC()
	summary, err := parseRollbackSummaryRecord(store.ToolResultRecord{
		ResultRef:   "tool_result:run_1:call_rollback",
		ToolName:    "rollback_workspace_checkpoint",
		Status:      store.ToolResultStatusFailed,
		ErrorReason: "workspace rollback conflict: tracked.txt",
		Preview:     `Tool call "rollback_workspace_checkpoint" failed: workspace rollback conflict: tracked.txt`,
		FullText:    `Tool call "rollback_workspace_checkpoint" failed: workspace rollback conflict: tracked.txt`,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("parseRollbackSummaryRecord: %v", err)
	}
	if summary.RollbackID != "tool_result:run_1:call_rollback" || summary.Status != "failed" {
		t.Fatalf("unexpected failed rollback summary: %+v", summary)
	}
	if summary.Error != "workspace rollback conflict: tracked.txt" {
		t.Fatalf("error = %q", summary.Error)
	}
}

func TestRuntimeWorkbenchServiceLoadFailsLoudOnResumeStatusError(t *testing.T) {
	traceStore, err := storesqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open trace store: %v", err)
	}
	t.Cleanup(func() {
		if err := traceStore.Close(); err != nil {
			t.Fatalf("close trace store: %v", err)
		}
	})
	store := &runtimeWorkbenchStoreStub{
		session: &events.SessionRecord{SessionID: "session_1", Title: "Runtime Workbench"},
		run: &events.RunRecord{
			RunID:        "run_missing",
			SessionID:    "session_1",
			Status:       events.RunStatusInterrupted,
			CheckpointID: "checkpoint_1",
		},
		summary: &model.SessionSummary{SessionID: "session_1", Summary: "ok"},
	}

	service := NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{}, store, NewTraceService(traceStore))
	workbench, err := service.Load(context.Background(), "session_1")
	if err == nil {
		t.Fatalf("expected resume status error, got workbench %+v", workbench)
	}
	if !strings.Contains(err.Error(), "load resume status for run run_missing:") {
		t.Fatalf("unexpected resume status error: %v", err)
	}
	if strings.Contains(err.Error(), "resumable") {
		t.Fatalf("resume status error should not be masked as resumable: %v", err)
	}
}

func TestRuntimeWorkbenchServiceLoadUsesTraceResumeStatusOnly(t *testing.T) {
	ctx := context.Background()
	store := openRuntimeWorkbenchSQLiteStore(t)
	if _, err := store.CreateSession(ctx, "session_1", "Runtime Workbench"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	const runID = "run_unknown_interrupt"
	if err := store.CreateRunWithSession(ctx, runID, "session_1", 1, "need approval", "checkpoint_1"); err != nil {
		t.Fatalf("CreateRunWithSession: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.interrupted", map[string]any{
		"interrupt": map[string]any{
			"contexts": []any{
				map[string]any{
					"id":            "ctx_root",
					"is_root_cause": true,
					"info": map[string]any{
						"kind": "manual_gate",
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "waiting"); err != nil {
		t.Fatalf("MarkInterrupted: %v", err)
	}

	service := NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{}, store, NewTraceService(store))
	workbench, err := service.Load(ctx, "session_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if workbench.Resumable {
		t.Fatalf("checkpoint-only unsupported interrupt must not be resumable: %+v", workbench)
	}
	if !strings.Contains(workbench.ResumeReason, `unsupported kind "manual_gate"`) {
		t.Fatalf("resume reason should come from trace resume status, got %q", workbench.ResumeReason)
	}
}

func TestRuntimeWorkbenchServiceLoadRejectsInterruptedRunWithoutTraceService(t *testing.T) {
	store := &runtimeWorkbenchStoreStub{
		session: &events.SessionRecord{SessionID: "session_1", Title: "Runtime Workbench"},
		run: &events.RunRecord{
			RunID:        "run_1",
			SessionID:    "session_1",
			Status:       events.RunStatusInterrupted,
			CheckpointID: "checkpoint_1",
		},
		summary: &model.SessionSummary{SessionID: "session_1", Summary: "ok"},
	}

	service := NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{}, store, nil)
	_, err := service.Load(context.Background(), "session_1")
	if err == nil || !strings.Contains(err.Error(), "trace service is nil") {
		t.Fatalf("expected missing trace service error, got %v", err)
	}
}

type runtimeWorkbenchStoreStub struct {
	session             *events.SessionRecord
	run                 *events.RunRecord
	eventsByRun         map[string][]events.EventRecord
	decision            *decision.Record
	plan                *model.Plan
	summary             *model.SessionSummary
	loadEventsErr       error
	toolResultsByRun    map[string][]store.ToolResultRecord
	artifactsByRun      map[string][]store.ArtifactRecord
	providerUsagesByRun map[string][]providers.UsageRecord
}

func (s *runtimeWorkbenchStoreStub) LoadSession(_ context.Context, _ string) (*events.SessionRecord, error) {
	return s.session, nil
}

func (s *runtimeWorkbenchStoreStub) LoadLatestRunForSession(_ context.Context, _ string) (*events.RunRecord, error) {
	return s.run, nil
}

func (s *runtimeWorkbenchStoreStub) LoadEvents(_ context.Context, runID string) ([]events.EventRecord, error) {
	if s.loadEventsErr != nil {
		return nil, s.loadEventsErr
	}
	return s.eventsByRun[runID], nil
}

func (s *runtimeWorkbenchStoreStub) LoadRunDecision(_ context.Context, _ string) (*decision.Record, error) {
	return s.decision, nil
}

func (s *runtimeWorkbenchStoreStub) LoadRuntimePlan(_ context.Context, _ string) (*model.Plan, error) {
	if s.plan == nil {
		return nil, store.ErrPlanNotFound
	}
	return s.plan, nil
}

func (s *runtimeWorkbenchStoreStub) GetSessionSummary(_ context.Context, _ string) (*model.SessionSummary, error) {
	return s.summary, nil
}

func (s *runtimeWorkbenchStoreStub) ListByRun(_ context.Context, runID string) ([]store.ToolResultRecord, error) {
	return s.toolResultsByRun[runID], nil
}

func (s *runtimeWorkbenchStoreStub) ListArtifactsByRun(_ context.Context, runID string) ([]store.ArtifactRecord, error) {
	return s.artifactsByRun[runID], nil
}

func (s *runtimeWorkbenchStoreStub) ListProviderUsagesByRun(_ context.Context, runID string) ([]providers.UsageRecord, error) {
	return s.providerUsagesByRun[runID], nil
}

func initGitRepoForWorkbenchTest(t *testing.T, root string) {
	t.Helper()
	runGitForWorkbenchTest(t, root, "init")
	runGitForWorkbenchTest(t, root, "config", "user.name", "Acorn Test")
	runGitForWorkbenchTest(t, root, "config", "user.email", "acorn@example.com")
}

func runGitForWorkbenchTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}

func openRuntimeWorkbenchSQLiteStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	store, err := storesqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	})
	return store
}
