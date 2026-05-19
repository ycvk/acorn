package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
)

func TestTraceServiceWorksWithoutExecutionConfig(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_trace_only"
	if err := store.CreateRun(context.Background(), runID, "hello", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append run.started: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.completed", map[string]any{
		"message": map[string]any{"role": "assistant", "content": "done"},
	}); err != nil {
		t.Fatalf("append run.completed: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, events.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	service := NewTraceService(store)
	trace, err := service.Trace(context.Background(), runID)
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}
	if trace.Run == nil || trace.Run.RunID != runID {
		t.Fatalf("unexpected trace run: %#v", trace.Run)
	}
	if trace.Summary == nil || !trace.Summary.Completed {
		t.Fatalf("unexpected trace summary: %#v", trace.Summary)
	}
}

func TestTraceServiceInfersResumeTargetsForGenericInterrupt(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_resume"
	if err := store.CreateRun(context.Background(), runID, "need approval", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.interrupted", map[string]any{
		"interrupt": map[string]any{
			"contexts": []any{
				map[string]any{"id": "ctx_root", "is_root_cause": true},
				map[string]any{"id": "ctx_child", "is_root_cause": false},
			},
		},
	}); err != nil {
		t.Fatalf("append run.interrupted: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "waiting for approval"); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}

	service := NewTraceService(store)
	status, err := service.ResumeStatus(context.Background(), runID)
	if err != nil {
		t.Fatalf("ResumeStatus: %v", err)
	}
	if !status.Resumable || len(status.InterruptIDs) != 1 || status.InterruptIDs[0] != "ctx_root" {
		t.Fatalf("unexpected resume status: %#v", status)
	}
	if !strings.Contains(status.Reason, "resumable via 1 pending actions") {
		t.Fatalf("unexpected reason: %q", status.Reason)
	}
	targets, err := service.InferResumeTargets(context.Background(), runID)
	if err != nil {
		t.Fatalf("InferResumeTargets: %v", err)
	}
	params, ok := targets["ctx_root"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected targets: %#v", targets)
	}
	if len(params) != 0 {
		t.Fatalf("unexpected generic interrupt payload: %#v", params)
	}
}

func TestTraceServiceInfersResumeTargetsForKnownRunCommandInterruptKinds(t *testing.T) {
	tests := []struct {
		name string
		info map[string]any
	}{
		{
			name: "run command pause",
			info: map[string]any{
				"kind":    "run_command_pause",
				"command": []any{"git", "status"},
				"cwd":     "/repo",
				"message": "run_command paused before execution; resume this interrupt to continue",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := openTestStore(t)
			defer store.Close()

			const runID = "run_resume_known_kind"
			if err := store.CreateRun(context.Background(), runID, "need approval", runID); err != nil {
				t.Fatalf("create run: %v", err)
			}
			if _, err := store.AppendEventContext(context.Background(), runID, "run.interrupted", map[string]any{
				"interrupt": map[string]any{
					"contexts": []any{
						map[string]any{
							"id":            "ctx_root",
							"is_root_cause": true,
							"info":          tc.info,
						},
					},
				},
			}); err != nil {
				t.Fatalf("append run.interrupted: %v", err)
			}
			if err := store.MarkInterruptedContext(context.Background(), runID, "waiting for approval"); err != nil {
				t.Fatalf("mark interrupted: %v", err)
			}

			service := NewTraceService(store)
			status, err := service.ResumeStatus(context.Background(), runID)
			if err != nil {
				t.Fatalf("ResumeStatus: %v", err)
			}
			if !status.Resumable {
				t.Fatalf("expected %q interrupt to be resumable: %#v", tc.name, status)
			}
			targets, err := service.InferResumeTargets(context.Background(), runID)
			if err != nil {
				t.Fatalf("InferResumeTargets: %v", err)
			}
			params, ok := targets["ctx_root"].(map[string]any)
			if !ok {
				t.Fatalf("unexpected targets: %#v", targets)
			}
			if len(params) != 0 {
				t.Fatalf("unexpected interrupt payload: %#v", params)
			}
		})
	}
}

func TestTraceServiceInferResumeTargetsRejectsUnknownInterruptKind(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_resume_unknown"
	if err := store.CreateRun(context.Background(), runID, "need approval", runID); err != nil {
		t.Fatalf("create run: %v", err)
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
		t.Fatalf("append run.interrupted: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "waiting for approval"); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}

	service := NewTraceService(store)
	_, err := service.InferResumeTargets(context.Background(), runID)
	if err == nil || !strings.Contains(err.Error(), `unsupported kind "manual_gate"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTraceServiceResumeStatusRejectsFailedRun(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_failed"
	if err := store.CreateRun(context.Background(), runID, "inspect repo", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, events.RunStatusFailed, "partial output", "shell exited with status 1"); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	service := NewTraceService(store)
	status, err := service.ResumeStatus(context.Background(), runID)
	if err != nil {
		t.Fatalf("ResumeStatus: %v", err)
	}
	if status.Resumable {
		t.Fatalf("failed run should not be resumable: %#v", status)
	}
	if status.Status != events.RunStatusFailed {
		t.Fatalf("status = %q, want failed", status.Status)
	}
	if !strings.Contains(status.Reason, "failed and cannot be resumed") {
		t.Fatalf("unexpected reason: %q", status.Reason)
	}

	_, err = service.InferResumeTargets(context.Background(), runID)
	if !errors.Is(err, runtime.ErrRunNotInterrupted) {
		t.Fatalf("InferResumeTargets error = %v, want runtime.ErrRunNotInterrupted", err)
	}
	if err == nil || !strings.Contains(err.Error(), "failed and cannot be resumed") {
		t.Fatalf("unexpected infer error: %v", err)
	}
}

func TestTraceServiceResumeStatusExplainsCompletedRun(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_completed"
	if err := store.CreateRun(context.Background(), runID, "summarize repo", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, events.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	service := NewTraceService(store)
	status, err := service.ResumeStatus(context.Background(), runID)
	if err != nil {
		t.Fatalf("ResumeStatus: %v", err)
	}
	if status.Resumable {
		t.Fatalf("completed run should not be resumable: %#v", status)
	}
	if status.Status != events.RunStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", status.Status)
	}
	if !strings.Contains(status.Reason, "completed and does not need resume") {
		t.Fatalf("unexpected reason: %q", status.Reason)
	}
}
