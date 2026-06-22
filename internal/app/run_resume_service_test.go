package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/domain"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestRunResumeServiceInfersResumeTargetsForGenericInterrupt(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_resume"
	if err := store.CreateRun(context.Background(), runID, "need approval"); err != nil {
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

	service := NewRunResumeService(store)
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

func TestRunResumeServiceInfersResumeTargetsForKnownRunCommandInterruptKinds(t *testing.T) {
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
			if err := store.CreateRun(context.Background(), runID, "need approval"); err != nil {
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

			service := NewRunResumeService(store)
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

func TestRunResumeServiceInfersResumeTargetsForDecidedOperatorQuestion(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_operator_question_resume"
	if err := store.CreateRun(context.Background(), runID, "need input"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.CreatePendingAction(context.Background(), storecore.CreatePendingActionInput{
		ActionID:    "action_operator_resume",
		RunID:       runID,
		Kind:        domain.PendingActionKindOperatorQuestion,
		PayloadJSON: `{"question":"Which path?","allow_freeform":true}`,
		Status:      domain.PendingActionStatusPending,
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}
	if _, err := store.DecidePendingAction(context.Background(), "action_operator_resume", domain.PendingActionStatusApproved, `{"action":"answer","answer":"ship it"}`); err != nil {
		t.Fatalf("decide pending action: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.interrupted", map[string]any{
		"interrupt": map[string]any{
			"contexts": []any{
				map[string]any{
					"id":            "ctx_operator",
					"is_root_cause": true,
					"info": map[string]any{
						"kind":      "operator_question",
						"action_id": "action_operator_resume",
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("append run.interrupted: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "waiting for operator"); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}

	targets, err := NewRunResumeService(store).InferResumeTargets(context.Background(), runID)
	if err != nil {
		t.Fatalf("InferResumeTargets: %v", err)
	}
	payload, ok := targets["ctx_operator"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected targets: %#v", targets)
	}
	if payload["action"] != "answer" || payload["answer"] != "ship it" || payload["action_id"] != "action_operator_resume" {
		t.Fatalf("unexpected operator resume payload: %#v", payload)
	}
}

func TestRunResumeServiceInferResumeTargetsRejectsUnknownInterruptKind(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_resume_unknown"
	if err := store.CreateRun(context.Background(), runID, "need approval"); err != nil {
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

	service := NewRunResumeService(store)
	_, err := service.InferResumeTargets(context.Background(), runID)
	if err == nil || !strings.Contains(err.Error(), `unsupported kind "manual_gate"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResumeServiceResumeStatusRejectsFailedRun(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_failed"
	if err := store.CreateRun(context.Background(), runID, "inspect repo"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, domain.RunStatusFailed, "partial output", "shell exited with status 1"); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	service := NewRunResumeService(store)
	status, err := service.ResumeStatus(context.Background(), runID)
	if err != nil {
		t.Fatalf("ResumeStatus: %v", err)
	}
	if status.Resumable {
		t.Fatalf("failed run should not be resumable: %#v", status)
	}
	if status.Status != domain.RunStatusFailed {
		t.Fatalf("status = %q, want failed", status.Status)
	}
	if !strings.Contains(status.Reason, "failed and cannot be resumed") {
		t.Fatalf("unexpected reason: %q", status.Reason)
	}

	_, err = service.InferResumeTargets(context.Background(), runID)
	if !errors.Is(err, domain.ErrRunNotInterrupted) {
		t.Fatalf("InferResumeTargets error = %v, want domain.ErrRunNotInterrupted", err)
	}
	if err == nil || !strings.Contains(err.Error(), "failed and cannot be resumed") {
		t.Fatalf("unexpected infer error: %v", err)
	}
}

func TestRunResumeServiceResumeStatusExplainsCompletedRun(t *testing.T) {
	store := openTestStore(t)

	const runID = "run_completed"
	if err := store.CreateRun(context.Background(), runID, "summarize repo"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), runID, domain.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	service := NewRunResumeService(store)
	status, err := service.ResumeStatus(context.Background(), runID)
	if err != nil {
		t.Fatalf("ResumeStatus: %v", err)
	}
	if status.Resumable {
		t.Fatalf("completed run should not be resumable: %#v", status)
	}
	if status.Status != domain.RunStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", status.Status)
	}
	if !strings.Contains(status.Reason, "completed and does not need resume") {
		t.Fatalf("unexpected reason: %q", status.Reason)
	}
}
