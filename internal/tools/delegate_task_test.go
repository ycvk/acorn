package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/orchestration"
)

type fakeSubagentExecutor struct {
	result  *orchestration.ChildAgentResult
	execErr error
	called  bool
	task    string
	req     orchestration.ChildAgentRequest
}

func (f *fakeSubagentExecutor) Execute(ctx context.Context, req orchestration.ChildAgentRequest) (*orchestration.ChildAgentResult, error) {
	f.called = true
	f.task = req.Task
	f.req = req
	return f.result, f.execErr
}

type fakeDelegateBridge struct{}

func (fakeDelegateBridge) CurrentRunID(ctx context.Context) string {
	return "parent_run_123"
}

func (fakeDelegateBridge) CurrentSessionID(context.Context) string {
	return "parent_session_123"
}

func mustInvokableDelegate(t *testing.T, tool einotool.BaseTool) einotool.InvokableTool {
	t.Helper()
	invokable, ok := tool.(einotool.InvokableTool)
	if !ok {
		t.Fatalf("delegate_task tool is not invokable")
	}
	return invokable
}

func TestDelegateToolExecute(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{
		ChildRunID:    "child_run_1",
		FinalStatus:   "succeeded",
		OutputSummary: "subagent completed successfully",
		Acceptance:    orchestration.ChildAgentAcceptance{Status: "passed"},
	}}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()

	output, err := invokable.InvokableRun(ctx, `{"task":"analyze the codebase","acceptance_criteria":["deliver a written summary"]}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(output, "subagent completed successfully") {
		t.Fatalf("output = %q, want to contain subagent result", output)
	}
	if !fake.called {
		t.Fatal("ExecuteSubagent was not called")
	}
	if fake.req.WorkspaceMode != orchestration.ChildWorkspaceModeShared {
		t.Fatalf("workspace mode = %q, want shared", fake.req.WorkspaceMode)
	}
}

func TestDelegateToolEmptyTask(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()

	_, err = invokable.InvokableRun(ctx, `{"task":"","acceptance_criteria":["summary"]}`)
	if err == nil {
		t.Fatal("expected error for empty task")
	}
	if !strings.Contains(err.Error(), "task is required") {
		t.Fatalf("error = %v, want task required error", err)
	}
}

func TestDelegateToolNoParentRunID(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{}}
	tool, err := NewDelegateTool(fake, nil)
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	_, err = invokable.InvokableRun(context.Background(), `{"task":"do something","acceptance_criteria":["done"]}`)
	if err == nil {
		t.Fatal("expected error for missing parent run ID")
	}
	if !strings.Contains(err.Error(), "no active run context") {
		t.Fatalf("error = %v, want no active run context error", err)
	}
}

func TestDelegateToolSubagentError(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{execErr: errors.New("subagent failed")}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()

	_, err = invokable.InvokableRun(ctx, `{"task":"do something","acceptance_criteria":["done"]}`)
	if err == nil {
		t.Fatal("expected error from subagent failure")
	}
	if !strings.Contains(err.Error(), "delegate task") {
		t.Fatalf("error = %v, want delegate task error", err)
	}
}

func TestDelegateToolWithContextField(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{
		ChildRunID:    "child_run_1",
		FinalStatus:   "succeeded",
		OutputSummary: "done",
		Acceptance:    orchestration.ChildAgentAcceptance{Status: "passed"},
	}}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()

	output, err := invokable.InvokableRun(ctx, `{"task":"analyze","context":"focus on auth module","acceptance_criteria":["report auth findings"]}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(output, "done") {
		t.Fatalf("output = %q, want to contain done", output)
	}
	if !fake.called {
		t.Fatal("ExecuteSubagent was not called")
	}
}

func TestDelegateToolNilExecutor(t *testing.T) {
	t.Parallel()

	_, err := NewDelegateTool(nil, fakeDelegateBridge{})
	if err == nil {
		t.Fatal("expected nil executor to fail at construction")
	}
	if !strings.Contains(err.Error(), "delegate_task requires child executor") {
		t.Fatalf("error = %v, want construction error", err)
	}
}

func TestDelegateToolExplicitWorktreeWorkspaceMode(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{
		ChildRunID:    "child_run_1",
		FinalStatus:   "succeeded",
		OutputSummary: "ok",
		Acceptance:    orchestration.ChildAgentAcceptance{Status: "passed"},
	}}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()
	_, err = invokable.InvokableRun(ctx, `{"task":"do something","workspace_mode":"worktree","acceptance_criteria":["done"]}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if fake.req.WorkspaceMode != orchestration.ChildWorkspaceModeWorktree {
		t.Fatalf("workspace mode = %q, want worktree", fake.req.WorkspaceMode)
	}
}

func TestDelegateToolPassesTaskToExecutor(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{
		ChildRunID:    "child_run_1",
		FinalStatus:   "succeeded",
		OutputSummary: "ok",
		Acceptance:    orchestration.ChildAgentAcceptance{Status: "passed"},
	}}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()

	_, err = invokable.InvokableRun(ctx, `{"task":"inspect the README","acceptance_criteria":["report README findings"]}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if fake.req.Task != "inspect the README" {
		t.Fatalf("task passed to executor = %q, want %q", fake.req.Task, "inspect the README")
	}
}

func TestDelegateToolPassesAllowedToolsToExecutor(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{
		ChildRunID:    "child_run_1",
		FinalStatus:   "succeeded",
		OutputSummary: "ok",
		Acceptance:    orchestration.ChildAgentAcceptance{Status: "passed"},
	}}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()
	_, err = invokable.InvokableRun(ctx, `{"task":"inspect the README","allowed_tools":["read_file","run_command"],"acceptance_criteria":["report README findings"]}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if got, want := strings.Join(fake.req.AllowedToolNames, ","), "read_file,run_command"; got != want {
		t.Fatalf("allowed tools = %q, want %q", got, want)
	}
}

func TestDelegateToolRequiresAcceptanceCriteria(t *testing.T) {
	t.Parallel()

	fake := &fakeSubagentExecutor{result: &orchestration.ChildAgentResult{
		ChildRunID:    "child_run_1",
		FinalStatus:   "succeeded",
		OutputSummary: "ok",
		Acceptance:    orchestration.ChildAgentAcceptance{Status: "passed"},
	}}
	tool, err := NewDelegateTool(fake, fakeDelegateBridge{})
	if err != nil {
		t.Fatalf("NewDelegateTool: %v", err)
	}
	invokable := mustInvokableDelegate(t, tool)

	ctx := context.Background()
	_, err = invokable.InvokableRun(ctx, `{"task":"inspect the README"}`)
	if err == nil || !strings.Contains(err.Error(), "acceptance_criteria is required") {
		t.Fatalf("error = %v, want acceptance_criteria required error", err)
	}
}
