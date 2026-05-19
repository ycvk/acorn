package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
)

func TestResumeServiceResumeKeepsNonApprovalInterruptDefaultTarget(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	const runID = "run_resume"
	if err := store.CreateRun(context.Background(), runID, "need approval", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.interrupted", map[string]any{
		"interrupt": map[string]any{
			"contexts": []any{
				map[string]any{"id": "ctx_root", "is_root_cause": true},
			},
		},
	}); err != nil {
		t.Fatalf("append run.interrupted: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "waiting for approval"); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}

	var (
		resumedRunID string
		resumed      map[string]any
	)
	service := NewResumeService(NewTraceService(store), executorFactoryFunc(func(_ context.Context) (executorHandle, error) {
		return resumeTestExecutorHandle{
			resumeWithTargetsFn: func(ctx context.Context, targetRunID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error) {
				resumedRunID = targetRunID
				resumed = targets
				return &runtime.Result{RunID: targetRunID, Status: events.RunStatusSucceeded, Output: "done"}, nil
			},
		}, nil
	}), store)

	result, err := service.Resume(ctx, runID, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result == nil || result.RunID != runID {
		t.Fatalf("unexpected result: %#v", result)
	}
	if resumedRunID != runID {
		t.Fatalf("resume called with runID %q, want %q", resumedRunID, runID)
	}
	params, ok := resumed["ctx_root"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected resume targets: %#v", resumed)
	}
	if len(params) != 0 {
		t.Fatalf("unexpected resume targets: %#v", resumed)
	}
}

func TestResumeServiceRejectsFailedRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	const runID = "run_failed"
	if err := store.CreateRun(context.Background(), runID, "inspect repo", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, events.RunStatusFailed, "partial output", "shell exited with status 1"); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	var resumed bool
	service := NewResumeService(NewTraceService(store), executorFactoryFunc(func(_ context.Context) (executorHandle, error) {
		return resumeTestExecutorHandle{
			resumeWithTargetsFn: func(ctx context.Context, targetRunID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error) {
				resumed = true
				return &runtime.Result{RunID: targetRunID, Status: events.RunStatusSucceeded}, nil
			},
		}, nil
	}), store)

	_, err := service.Resume(ctx, runID, nil)
	if !errors.Is(err, runtime.ErrRunNotInterrupted) {
		t.Fatalf("Resume error = %v, want runtime.ErrRunNotInterrupted", err)
	}
	if err == nil || !strings.Contains(err.Error(), "failed and cannot be resumed") {
		t.Fatalf("unexpected resume error: %v", err)
	}
	if resumed {
		t.Fatalf("executor should not be called for failed run")
	}
}

func TestResumeServiceRejectsCompletedRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	const runID = "run_completed"
	if err := store.CreateRun(context.Background(), runID, "inspect repo", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, events.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	var resumed bool
	service := NewResumeService(NewTraceService(store), executorFactoryFunc(func(_ context.Context) (executorHandle, error) {
		return resumeTestExecutorHandle{
			resumeWithTargetsFn: func(ctx context.Context, targetRunID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error) {
				resumed = true
				return &runtime.Result{RunID: targetRunID, Status: events.RunStatusSucceeded}, nil
			},
		}, nil
	}), store)

	_, err := service.Resume(ctx, runID, nil)
	if !errors.Is(err, runtime.ErrRunNotInterrupted) {
		t.Fatalf("Resume error = %v, want runtime.ErrRunNotInterrupted", err)
	}
	if err == nil || !strings.Contains(err.Error(), "completed and does not need resume") {
		t.Fatalf("unexpected resume error: %v", err)
	}
	if resumed {
		t.Fatalf("executor should not be called for completed run")
	}
}

type resumeTestExecutorHandle struct {
	resumeWithTargetsFn func(ctx context.Context, runID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error)
}

func (h resumeTestExecutorHandle) Run(ctx context.Context, input, skillID string, sink runtime.StreamSink) (*runtime.Result, error) {
	panic("unexpected Run call")
}

func (h resumeTestExecutorHandle) ExecuteMessages(ctx context.Context, req runtime.ExecuteRequest, sink runtime.StreamSink) (*runtime.Result, error) {
	panic("unexpected ExecuteMessages call")
}

func (h resumeTestExecutorHandle) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error) {
	if h.resumeWithTargetsFn == nil {
		panic("unexpected ResumeWithTargets call")
	}
	return h.resumeWithTargetsFn(ctx, runID, targets, sink)
}
