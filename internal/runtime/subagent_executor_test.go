package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/store"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/stream"
)

func openStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSubagentExecutorEmptyTask(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{
		depths: make(map[string]int),
	}

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

	se := &SubagentExecutor{
		depths: make(map[string]int),
	}

	_, err := se.Execute(context.Background(), orchestration.ChildAgentRequest{
		ParentRunID:        "",
		Task:               "task",
		AcceptanceCriteria: []string{"done"},
	})
	if err == nil || !strings.Contains(err.Error(), "parent run ID is required") {
		t.Fatalf("error = %v, want parent run ID required error", err)
	}
}

func TestSubagentExecutorDepthTracking(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{
		depths: make(map[string]int),
	}

	parentRunID := "run_depth_test"

	if d := se.currentDepth(parentRunID); d != 0 {
		t.Fatalf("initial depth = %d, want 0", d)
	}

	d1 := se.incrementDepth(parentRunID)
	if d1 != 1 {
		t.Fatalf("first increment = %d, want 1", d1)
	}

	d2 := se.incrementDepth(parentRunID)
	if d2 != 2 {
		t.Fatalf("second increment = %d, want 2", d2)
	}

	se.decrementDepth(parentRunID)
	if d := se.currentDepth(parentRunID); d != 1 {
		t.Fatalf("after decrement = %d, want 1", d)
	}
}

func TestSubagentPayloadStreamKind(t *testing.T) {
	t.Parallel()

	started := stream.SubagentStartedPayload{SubRunID: "sub1", ParentID: "p1", Depth: 1, Task: "t", ChildRunMode: "fork", WorkspaceMode: "worktree", ContextMessages: 2}
	if started.StreamKind() != stream.StreamKindSubagentStarted {
		t.Fatalf("stream.SubagentStartedPayload.StreamKind() = %q, want %q", started.StreamKind(), stream.StreamKindSubagentStarted)
	}

	completed := stream.SubagentCompletedPayload{
		SubRunID:         "sub1",
		ParentID:         "p1",
		Summary:          "s",
		FinalStatus:      "succeeded",
		AcceptanceStatus: "passed",
		ChildRunMode:     "fork",
		WorkspaceMode:    "worktree",
		EvidenceRefs:     []string{"tool_result:run_child:call_1"},
	}
	if completed.StreamKind() != stream.StreamKindSubagentCompleted {
		t.Fatalf("stream.SubagentCompletedPayload.StreamKind() = %q, want %q", completed.StreamKind(), stream.StreamKindSubagentCompleted)
	}

	failed := stream.SubagentFailedPayload{
		SubRunID:         "sub1",
		ParentID:         "p1",
		Error:            "e",
		AcceptanceStatus: "failed",
		ChildRunMode:     "fork",
		WorkspaceMode:    "worktree",
	}
	if failed.StreamKind() != stream.StreamKindSubagentFailed {
		t.Fatalf("stream.SubagentFailedPayload.StreamKind() = %q, want %q", failed.StreamKind(), stream.StreamKindSubagentFailed)
	}
}

func TestSubagentPayloadUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind stream.StreamItemKind
	}{
		{stream.StreamKindSubagentStarted},
		{stream.StreamKindSubagentCompleted},
		{stream.StreamKindSubagentFailed},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			p, err := stream.UnmarshalPayload(tt.kind, []byte(`{}`))
			if err != nil {
				t.Fatalf("stream.UnmarshalPayload(%q): %v", tt.kind, err)
			}
			if p.StreamKind() != tt.kind {
				t.Fatalf("StreamKind() = %q, want %q", p.StreamKind(), tt.kind)
			}
		})
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
				Payload: &stream.SubagentStartedPayload{SubRunID: "sub_1", ParentID: "run_1", Depth: 1, Task: "analyze", ChildRunMode: "fork", WorkspaceMode: "worktree", ContextMessages: 2},
			},
			wantEventKind: "subagent.started",
		},
		{
			name: "subagent_completed",
			item: stream.StreamItem{
				RunID: "run_1", Sequence: 2, Kind: stream.StreamKindSubagentCompleted, CreatedAt: now,
				Payload: &stream.SubagentCompletedPayload{
					SubRunID:         "sub_1",
					ParentID:         "run_1",
					Summary:          "done",
					FinalStatus:      "succeeded",
					AcceptanceStatus: "passed",
					ChildRunMode:     "fork",
					WorkspaceMode:    "worktree",
					EvidenceRefs:     []string{"tool_result:run_child:call_1"},
				},
			},
			wantEventKind: "subagent.completed",
		},
		{
			name: "subagent_failed",
			item: stream.StreamItem{
				RunID: "run_1", Sequence: 3, Kind: stream.StreamKindSubagentFailed, CreatedAt: now,
				Payload: &stream.SubagentFailedPayload{
					SubRunID:         "sub_1",
					ParentID:         "run_1",
					Error:            "boom",
					AcceptanceStatus: "failed",
					ChildRunMode:     "fork",
					WorkspaceMode:    "worktree",
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

func TestSubagentEventKindToStreamKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		eventKind string
		want      stream.StreamItemKind
	}{
		{"subagent.started", stream.StreamKindSubagentStarted},
		{"subagent.completed", stream.StreamKindSubagentCompleted},
		{"subagent.failed", stream.StreamKindSubagentFailed},
	}

	for _, tt := range tests {
		t.Run(tt.eventKind, func(t *testing.T) {
			got := eventKindToStreamKind(tt.eventKind)
			if got != tt.want {
				t.Fatalf("eventKindToStreamKind(%q) = %q, want %q", tt.eventKind, got, tt.want)
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
			Payload: &stream.SubagentStartedPayload{SubRunID: "sub_1", ParentID: "run_1", Depth: 1, Task: "analyze the codebase", ChildRunMode: "fork", WorkspaceMode: "worktree", ContextMessages: 2},
		},
		{
			RunID: "run_1", Sequence: 2, Kind: stream.StreamKindSubagentCompleted, CreatedAt: now,
			Payload: &stream.SubagentCompletedPayload{
				SubRunID:         "sub_1",
				ParentID:         "run_1",
				Summary:          "found 3 issues",
				FinalStatus:      "succeeded",
				AcceptanceStatus: "passed",
				ChildRunMode:     "fork",
				WorkspaceMode:    "worktree",
				EvidenceRefs:     []string{"tool_result:run_child:call_1"},
			},
		},
		{
			RunID: "run_1", Sequence: 3, Kind: stream.StreamKindSubagentFailed, CreatedAt: now,
			Payload: &stream.SubagentFailedPayload{
				SubRunID:         "sub_1",
				ParentID:         "run_1",
				Error:            "timeout",
				AcceptanceStatus: "failed",
				ChildRunMode:     "fork",
				WorkspaceMode:    "worktree",
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
			if decoded.Payload.StreamKind() != original.Kind {
				t.Fatalf("Payload.StreamKind() = %q, want %q", decoded.Payload.StreamKind(), original.Kind)
			}
		})
	}
}

func TestSubagentAdapterExtractsRunID(t *testing.T) {
	t.Parallel()

	se := &SubagentExecutor{
		depths: make(map[string]int),
	}
	adapter := subagentExecutorAdapter{exec: se}

	ctx := withRunID(context.Background(), "test_parent")

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

	se := &SubagentExecutor{
		depths: make(map[string]int),
	}
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

	store := openStore(t)
	if err := store.CreateRun(context.Background(), "parent_run", "inspect repo", "parent_run"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	sinkCalls := 0
	ctx := stream.WithStreamSink(context.Background(), func(item stream.StreamItem) error {
		sinkCalls++
		if sinkCalls == 1 {
			return nil
		}
		return errors.New("sink broke")
	})

	se := &SubagentExecutor{
		store:  store,
		depths: make(map[string]int),
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

func TestSubagentTraceSummary(t *testing.T) {
	t.Parallel()

	items := []stream.StreamItem{
		{Kind: stream.StreamKindSubagentStarted},
		{Kind: stream.StreamKindSubagentCompleted},
		{Kind: stream.StreamKindSubagentFailed},
	}

	summary := summarizeStreamItems(items)
	if summary.ItemCount != 3 {
		t.Fatalf("ItemCount = %d, want 3", summary.ItemCount)
	}
}

func TestSubagentEventRecordRoundtrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	original := stream.StreamItem{
		RunID: "run_rt", Sequence: 1, Kind: stream.StreamKindSubagentStarted, CreatedAt: now,
		Payload: &stream.SubagentStartedPayload{SubRunID: "sub_1", ParentID: "run_rt", Depth: 1, Task: "inspect", ChildRunMode: "fork", WorkspaceMode: "worktree", ContextMessages: 2},
	}

	eventKind, payload, err := stream.ProjectStreamItemToEvent(original)
	if err != nil {
		t.Fatalf("stream.ProjectStreamItemToEvent: %v", err)
	}

	event := events.EventRecord{
		RunID:     original.RunID,
		Sequence:  original.Sequence,
		Kind:      eventKind,
		Payload:   payload,
		CreatedAt: original.CreatedAt,
	}

	reconstructed := projectEventToStreamItem(event)
	if reconstructed.Kind != stream.StreamKindSubagentStarted {
		t.Fatalf("reconstructed Kind = %q, want %q", reconstructed.Kind, stream.StreamKindSubagentStarted)
	}

	started, ok := reconstructed.Payload.(*stream.SubagentStartedPayload)
	if !ok {
		t.Fatalf("Payload type = %T, want *stream.SubagentStartedPayload", reconstructed.Payload)
	}
	if started.SubRunID != "sub_1" {
		t.Fatalf("SubRunID = %q, want %q", started.SubRunID, "sub_1")
	}
	if started.Depth != 1 {
		t.Fatalf("Depth = %d, want 1", started.Depth)
	}
	if started.ChildRunMode != "fork" || started.ContextMessages != 2 {
		t.Fatalf("lineage fields = mode:%q context:%d", started.ChildRunMode, started.ContextMessages)
	}
	if started.WorkspaceMode != "worktree" {
		t.Fatalf("WorkspaceMode = %q, want worktree", started.WorkspaceMode)
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

	plan := &store.PlanRecord{
		PlanID:    "plan_1",
		SessionID: "sess_1",
		RunID:     "run_1",
		Steps: []store.PlanStep{
			{
				ID: "s1",
				Evidence: []store.PlanEvidence{
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

	record := &store.PlanRecord{
		Steps: []store.PlanStep{{
			Evidence: []store.PlanEvidence{
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
