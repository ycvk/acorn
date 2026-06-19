package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

func TestSubagentExecutorEmptyTask(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{}

	_, err := se.Execute(context.Background(), orchestration.ChildAgentRequest{
		ParentRunID:        "parent_run",
		Task:               "",
		AcceptanceCriteria: []string{"done"},
	})
	if err == nil || !strings.Contains(err.Error(), "task is required") {
		t.Fatalf("error = %v, want task required error", err)
	}
}

func TestSubagentExecutorEmptyParentRunID(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{}

	_, err := se.Execute(context.Background(), orchestration.ChildAgentRequest{
		ParentRunID:        "",
		Task:               "task",
		AcceptanceCriteria: []string{"done"},
	})
	if err == nil || !strings.Contains(err.Error(), "parent run ID is required") {
		t.Fatalf("error = %v, want parent run ID required error", err)
	}
}

func TestSubagentExecutorDepthFromResolver(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{
		parentDepth: func(parentRunID string) int {
			if parentRunID == "deep_parent" {
				return 2
			}
			return 0
		},
	}

	if d := se.currentDepth("shallow"); d != 0 {
		t.Fatalf("shallow depth = %d, want 0", d)
	}
	if d := se.currentDepth("deep_parent"); d != 2 {
		t.Fatalf("deep depth = %d, want 2", d)
	}
}

func TestSubagentExecutorRejectsDepthOverLimit(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{
		// Parent already at the limit, so the child would exceed it.
		parentDepth: func(string) int { return defaultMaxSubagentDepth },
	}

	_, err := se.Execute(context.Background(), orchestration.ChildAgentRequest{
		ParentRunID: "deep_parent",
		Task:        "recurse",
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds configured limit") {
		t.Fatalf("error = %v, want depth limit error", err)
	}
}

func TestSubagentStreamItemProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		item          stream.StreamItem
		wantEventKind string
	}{
		{
			name: "subagent_started",
			item: stream.StreamItem{
				RunID: "run_1", Sequence: 1, Kind: stream.StreamKindSubagentStarted, CreatedAt: now,
				Payload: map[string]any{"sub_run_id": "sub_1", "parent_id": "run_1", "depth": 1, "task": "analyze", "child_run_mode": "fork", "workspace_mode": "worktree", "context_messages": 2},
			},
			wantEventKind: "subagent.started",
		},
		{
			name: "subagent_completed",
			item: stream.StreamItem{
				RunID: "run_1", Sequence: 2, Kind: stream.StreamKindSubagentCompleted, CreatedAt: now,
				Payload: map[string]any{
					"sub_run_id":        "sub_1",
					"parent_id":         "run_1",
					"summary":           "done",
					"final_status":      "succeeded",
					"acceptance_status": "passed",
					"child_run_mode":    "fork",
					"workspace_mode":    "worktree",
					"evidence_refs":     []string{"tool_result:run_child:call_1"},
				},
			},
			wantEventKind: "subagent.completed",
		},
		{
			name: "subagent_failed",
			item: stream.StreamItem{
				RunID: "run_1", Sequence: 3, Kind: stream.StreamKindSubagentFailed, CreatedAt: now,
				Payload: map[string]any{
					"sub_run_id":        "sub_1",
					"parent_id":         "run_1",
					"error":             "boom",
					"acceptance_status": "failed",
					"child_run_mode":    "fork",
					"workspace_mode":    "worktree",
				},
			},
			wantEventKind: "subagent.failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventKind, _, err := stream.ProjectStreamItemToEvent(tt.item)
			if err != nil {
				t.Fatalf("stream.ProjectStreamItemToEvent: %v", err)
			}
			if eventKind != tt.wantEventKind {
				t.Fatalf("event kind = %q, want %q", eventKind, tt.wantEventKind)
			}
		})
	}
}

func TestSubagentStreamItemJSONRoundtrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	items := []stream.StreamItem{
		{
			RunID: "run_1", Sequence: 1, Kind: stream.StreamKindSubagentStarted, CreatedAt: now,
			Payload: map[string]any{"sub_run_id": "sub_1", "parent_id": "run_1", "depth": 1, "task": "analyze the codebase", "child_run_mode": "fork", "workspace_mode": "worktree", "context_messages": 2},
		},
		{
			RunID: "run_1", Sequence: 2, Kind: stream.StreamKindSubagentCompleted, CreatedAt: now,
			Payload: map[string]any{
				"sub_run_id":        "sub_1",
				"parent_id":         "run_1",
				"summary":           "found 3 issues",
				"final_status":      "succeeded",
				"acceptance_status": "passed",
				"child_run_mode":    "fork",
				"workspace_mode":    "worktree",
				"evidence_refs":     []string{"tool_result:run_child:call_1"},
			},
		},
		{
			RunID: "run_1", Sequence: 3, Kind: stream.StreamKindSubagentFailed, CreatedAt: now,
			Payload: map[string]any{
				"sub_run_id":        "sub_1",
				"parent_id":         "run_1",
				"error":             "timeout",
				"acceptance_status": "failed",
				"child_run_mode":    "fork",
				"workspace_mode":    "worktree",
			},
		},
	}

	for _, original := range items {
		t.Run(string(original.Kind), func(t *testing.T) {
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var decoded stream.StreamItem
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if decoded.Kind != original.Kind {
				t.Fatalf("Kind = %q, want %q", decoded.Kind, original.Kind)
			}
			if decoded.RunID != original.RunID {
				t.Fatalf("RunID = %q, want %q", decoded.RunID, original.RunID)
			}
			if decoded.Payload == nil {
				t.Fatal("Payload is nil after roundtrip")
			}
		})
	}
}

func TestSubagentAdapterExtractsRunID(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{}
	adapter := subagentExecutorAdapter{exec: se}

	ctx := runtimeapi.WithRunID(context.Background(), "test_parent")

	_, err := adapter.ExecuteMessages(ctx, []*schema.Message{
		schema.UserMessage("test message"),
	})
	// Execution fails because store is nil, but depth should be properly decremented
	_ = err

	if d := se.currentDepth("test_parent"); d != 0 {
		t.Fatalf("depth after execution = %d, want 0 (deferred decrement)", d)
	}
}

func TestSubagentAdapterDefaultParentRunID(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{}
	adapter := subagentExecutorAdapter{exec: se}

	_, err := adapter.ExecuteMessages(context.Background(), []*schema.Message{
		schema.UserMessage("test"),
	})
	_ = err

	if d := se.currentDepth("sampling_parent"); d != 0 {
		t.Fatalf("depth after execution = %d, want 0", d)
	}
}

func TestSubagentEmitFailedReturnsDurableWriteError(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{}
	err := se.emitFailed(
		context.Background(),
		"parent_run",
		"sub_run",
		"delegate_sub_run",
		"step_1",
		orchestration.ChildRunModeFork,
		orchestration.ChildWorkspaceModeWorktree,
		"",
		events.ModeSingleAgent,
		"boom",
		nil,
	)
	if err == nil {
		t.Fatal("expected emitFailed to return error when store append fails")
	}
	if !strings.Contains(err.Error(), "emit subagent.failed") || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubagentExecuteJoinsEmitFailedError(t *testing.T) {
	t.Parallel()

	store, _ := newRunnerFactoryMemoryTestContext(t)

	sinkCalls := 0
	ctx := stream.WithStreamSink(context.Background(), func(item stream.StreamItem) error {
		sinkCalls++
		if sinkCalls == 1 {
			return nil
		}
		return errors.New("sink broke")
	})

	se := &SubagentExecutor{
		store: store,
	}
	_, err := se.Execute(ctx, orchestration.ChildAgentRequest{
		ParentRunID: "parent_run",
		Task:        "inspect repo",
	})
	if err == nil {
		t.Fatal("expected execution error")
	}
	if !strings.Contains(err.Error(), "create subagent executor: config is required") {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if !strings.Contains(err.Error(), "emit subagent.failed") {
		t.Fatalf("expected joined emit subagent.failed error, got %v", err)
	}
}

func TestEvaluateDelegationAcceptancePassed(t *testing.T) {
	t.Parallel()

	spec := orchestration.ChildAgentRequest{
		Task:               "write tests",
		AcceptanceCriteria: []string{"tests pass"},
		ExpectedEvidence:   []string{"go test ./internal/auth passed"},
	}
	acceptance := evaluateDelegationAcceptance(
		spec,
		events.RunStatusSucceeded,
		"tests pass with updated auth coverage",
		[]string{"go test ./internal/auth passed", "updated internal/auth/handler_test.go"},
		nil,
	)
	if acceptance.Status != "passed" {
		t.Fatalf("status = %q, want passed", acceptance.Status)
	}
	if len(acceptance.Reasons) != 0 {
		t.Fatalf("reasons = %+v, want none", acceptance.Reasons)
	}
}

func TestEvaluateDelegationAcceptanceFailed(t *testing.T) {
	t.Parallel()

	spec := orchestration.ChildAgentRequest{
		Task:               "write tests",
		AcceptanceCriteria: []string{"tests pass"},
		ExpectedEvidence:   []string{"go test ./internal/auth passed"},
	}
	acceptance := evaluateDelegationAcceptance(
		spec,
		events.RunStatusInterrupted,
		"tests not run",
		[]string{"updated internal/auth/handler_test.go"},
		nil,
	)
	if acceptance.Status != "failed" {
		t.Fatalf("status = %q, want failed", acceptance.Status)
	}
	if len(acceptance.Reasons) < 2 {
		t.Fatalf("reasons = %+v, want multiple failure reasons", acceptance.Reasons)
	}
	if !strings.Contains(strings.Join(acceptance.Reasons, " | "), "missing expected evidence") {
		t.Fatalf("reasons = %+v, want missing expected evidence", acceptance.Reasons)
	}
}

func TestEvaluateDelegationAcceptanceFailsOnChildPlanFailure(t *testing.T) {
	spec := orchestration.ChildAgentRequest{Task: "write file"}
	acceptance := evaluateDelegationAcceptance(
		spec,
		events.RunStatusFailed,
		"Plan finished.",
		nil,
		[]string{"child plan step create file failed"},
	)
	if acceptance.Status != "failed" {
		t.Fatalf("status = %q, want failed", acceptance.Status)
	}
	if !summaryContains(acceptance.Reasons, "child plan step create file failed") {
		t.Fatalf("reasons = %+v", acceptance.Reasons)
	}
}

func TestDelegationEvidenceSummaries(t *testing.T) {
	t.Parallel()

	plan := &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_1",
		RunID:     "run_1",
		Steps: []model.PlanStep{
			{
				ID: "s1",
				Evidence: []model.PlanEvidence{
					{Summary: "go test ./internal/auth passed"},
					{Summary: "go test ./internal/auth passed"},
					{Summary: "updated internal/auth/handler_test.go"},
				},
			},
		},
	}
	got := delegationEvidenceSummaries(plan)
	if len(got) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(got))
	}
	if got[0] != "go test ./internal/auth passed" {
		t.Fatalf("first summary = %q, want go test ./internal/auth passed", got[0])
	}
}

func TestDelegationEvidenceRefsUseChildPlanEvidence(t *testing.T) {
	t.Parallel()

	record := &model.Plan{
		Steps: []model.PlanStep{{
			Evidence: []model.PlanEvidence{
				{
					ID:            "ev_1",
					ToolResultRef: "tool_result:run_child:call_1",
					ChildRunID:    "run_nested",
				},
				{
					ID:            "ev_2",
					ToolResultRef: "tool_result:run_child:call_1",
				},
			},
		}},
	}

	refs := delegationEvidenceRefs(record)
	want := []string{"tool_result:run_child:call_1", "run:run_nested", "evidence:ev_1", "evidence:ev_2"}
	if strings.Join(refs, ",") != strings.Join(want, ",") {
		t.Fatalf("refs = %+v, want %+v", refs, want)
	}
}
