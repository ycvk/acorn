package runtime

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/orchestration"
)

func TestValidatePlanEvidenceRejectsDiffWithoutRefOrPaths(t *testing.T) {
	err := validatePlanEvidence("s1", PlanEvidence{
		ID:          "ev_1",
		StepID:      "s1",
		Kind:        EvidenceKindDiff,
		Status:      EvidenceStatusPassed,
		Summary:     "diff found",
		SourceRunID: "run_1",
		RecordedAt:  time.Now().UTC(),
	})
	if err == nil || err.Error() == "" {
		t.Fatal("expected diff validation error")
	}
}

func TestEnsureVerificationIntentCoverageFailsOnRecordedOnlyEvidence(t *testing.T) {
	err := ensureVerificationIntentCoverage(PlanStep{
		ID: "s1",
		VerificationIntent: []VerificationIntent{{
			Kind:   "test",
			Reason: "prove with tests",
		}},
		Evidence: []PlanEvidence{{
			ID:          "ev_1",
			StepID:      "s1",
			Kind:        EvidenceKindTool,
			Status:      EvidenceStatusRecorded,
			Summary:     "read something",
			SourceRunID: "run_1",
			RecordedAt:  time.Now().UTC(),
		}},
	})
	if !errors.Is(err, ErrPlanStepVerificationGap) {
		t.Fatalf("error = %v, want ErrPlanStepVerificationGap", err)
	}
}

func TestEnsureVerificationIntentCoveragePassesWithTestEvidence(t *testing.T) {
	err := ensureVerificationIntentCoverage(PlanStep{
		ID: "s1",
		VerificationIntent: []VerificationIntent{{
			Kind:    "test",
			Command: []string{"go", "test", "./internal/runtime"},
			Paths:   []string{"internal/runtime"},
			Reason:  "prove with tests",
		}},
		Evidence: []PlanEvidence{{
			ID:          "ev_1",
			StepID:      "s1",
			Kind:        EvidenceKindTest,
			Status:      EvidenceStatusPassed,
			Summary:     "go test passed",
			Command:     []string{"go", "test", "./internal/runtime"},
			Paths:       []string{"internal/runtime"},
			SourceRunID: "run_1",
			RecordedAt:  time.Now().UTC(),
		}},
	})
	if err != nil {
		t.Fatalf("ensureVerificationIntentCoverage: %v", err)
	}
}

func TestValidatePlanEvidenceAcceptsVerifierEvidence(t *testing.T) {
	err := validatePlanEvidence("s1", PlanEvidence{
		ID:          "ev_1",
		StepID:      "s1",
		Kind:        EvidenceKindVerifier,
		Status:      EvidenceStatusPassed,
		Summary:     "verifier passed",
		ChildRunID:  "run_child_1",
		SourceRunID: "run_parent",
		RecordedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("validatePlanEvidence: %v", err)
	}
}

func TestValidatePlanEvidenceAcceptsCheckpointAndRollbackEvidence(t *testing.T) {
	now := time.Now().UTC()
	for _, evidence := range []PlanEvidence{
		{
			ID:          "ev_checkpoint",
			StepID:      "s1",
			Kind:        EvidenceKindCheckpoint,
			Status:      EvidenceStatusPassed,
			Summary:     "checkpoint recorded",
			SourceRunID: "run_parent",
			RecordedAt:  now,
		},
		{
			ID:          "ev_rollback",
			StepID:      "s1",
			Kind:        EvidenceKindRollback,
			Status:      EvidenceStatusFailed,
			Summary:     "rollback failed",
			SourceRunID: "run_parent",
			RecordedAt:  now,
		},
	} {
		if err := validatePlanEvidence("s1", evidence); err != nil {
			t.Fatalf("validatePlanEvidence(%s): %v", evidence.Kind, err)
		}
	}
}

func TestEvidenceForToolMessageDerivesCheckpointEvidence(t *testing.T) {
	recordedAt := time.Now().UTC()
	items, err := evidenceForToolMessage(toolMessageEvidenceInput{
		Step:          PlanStep{ID: "s1"},
		RunID:         "run_parent",
		ToolName:      "create_file",
		ToolCallID:    "call_1",
		ArgumentsJSON: `{"path":"notes.txt","content":"hello"}`,
		Message: &planToolMessage{
			Content: `{"path":"` + "notes.txt" + `","bytes":5,"message":"ok","checkpoint_id":"workspace_checkpoint_1","checkpoint_paths":["notes.txt"],"verified_bytes":5,"verified_content":"hello","verification_truncated":false}`,
			Extra: map[string]any{
				"tool_result_ref": "tool_result:run_parent:call_1",
			},
		},
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("evidenceForToolMessage: %v", err)
	}
	var checkpoint *PlanEvidence
	for i := range items {
		if items[i].Kind == EvidenceKindCheckpoint {
			checkpoint = &items[i]
			break
		}
	}
	if checkpoint == nil {
		t.Fatalf("checkpoint evidence missing: %+v", items)
	}
	if checkpoint.Status != EvidenceStatusPassed || len(checkpoint.Paths) != 1 || checkpoint.Paths[0] != "notes.txt" {
		t.Fatalf("checkpoint evidence = %+v", checkpoint)
	}
	if checkpoint.ToolResultRef != "tool_result:run_parent:call_1" {
		t.Fatalf("checkpoint tool_result_ref = %q", checkpoint.ToolResultRef)
	}
}

func TestEvidenceForToolMessageDerivesRunVerificationEvidence(t *testing.T) {
	recordedAt := time.Now().UTC()
	items, err := evidenceForToolMessage(toolMessageEvidenceInput{
		Step:          PlanStep{ID: "s1"},
		RunID:         "run_parent",
		ToolName:      "run_verification",
		ToolCallID:    "call_verify",
		ArgumentsJSON: `{"kind":"test","command":["go","test","./internal/runtime"],"paths":["internal/runtime"]}`,
		Message: &planToolMessage{
			Content: `{"kind":"test","status":"failed","summary":"test verification failed with exit code 1","command":["go","test","./internal/runtime"],"exit_code":1,"paths":["internal/runtime"],"stdout_artifact_id":"artifact_stdout","stderr_artifact_id":"artifact_stderr"}`,
			Extra: map[string]any{
				"tool_result_ref": "tool_result:run_parent:call_verify",
			},
		},
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("evidenceForToolMessage: %v", err)
	}
	var verification *PlanEvidence
	for i := range items {
		if items[i].Kind == EvidenceKindTest {
			verification = &items[i]
			break
		}
	}
	if verification == nil {
		t.Fatalf("run_verification evidence missing: %+v", items)
	}
	if verification.Status != EvidenceStatusFailed || verification.Error == "" {
		t.Fatalf("verification evidence = %+v", verification)
	}
	if strings.Join(verification.Command, " ") != "go test ./internal/runtime" {
		t.Fatalf("verification command = %+v", verification.Command)
	}
	if verification.ToolResultRef != "tool_result:run_parent:call_verify" {
		t.Fatalf("verification tool_result_ref = %q", verification.ToolResultRef)
	}
}

func TestEvidenceForToolMessageDerivesGitSummaryDiffEvidence(t *testing.T) {
	recordedAt := time.Now().UTC()
	items, err := evidenceForToolMessage(toolMessageEvidenceInput{
		Step:          PlanStep{ID: "s1"},
		RunID:         "run_parent",
		ToolName:      "git_summary",
		ToolCallID:    "call_git",
		ArgumentsJSON: `{"include_diff":true}`,
		Message: &planToolMessage{
			Content: `{"clean":false,"changed_paths":["a.go"],"diff_artifact_id":"artifact_diff"}`,
			Extra: map[string]any{
				"tool_result_ref": "tool_result:run_parent:call_git",
			},
		},
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("evidenceForToolMessage: %v", err)
	}
	var diff *PlanEvidence
	for i := range items {
		if items[i].Kind == EvidenceKindDiff {
			diff = &items[i]
			break
		}
	}
	if diff == nil {
		t.Fatalf("git_summary diff evidence missing: %+v", items)
	}
	if diff.DiffRef != "artifact_diff" || strings.Join(diff.Paths, ",") != "a.go" {
		t.Fatalf("diff evidence = %+v", diff)
	}
	if diff.ToolResultRef != "tool_result:run_parent:call_git" {
		t.Fatalf("diff tool_result_ref = %q", diff.ToolResultRef)
	}
}

func TestEvidenceForToolMessageDerivesRollbackEvidence(t *testing.T) {
	recordedAt := time.Now().UTC()
	items, err := evidenceForToolMessage(toolMessageEvidenceInput{
		Step:          PlanStep{ID: "s1"},
		RunID:         "run_parent",
		ToolName:      "rollback_workspace_checkpoint",
		ToolCallID:    "call_2",
		ArgumentsJSON: `{"checkpoint_id":"workspace_checkpoint_1"}`,
		Message: &planToolMessage{
			Content: `{"checkpoint_id":"workspace_checkpoint_1","rollback_id":"workspace_rollback_1","status":"failed","restored_paths":[],"conflict_paths":["notes.txt"],"error":"workspace rollback conflict"}`,
			Extra: map[string]any{
				"tool_result_ref":   "tool_result:run_parent:call_2",
				"tool_error":        true,
				"tool_error_reason": "workspace rollback conflict",
			},
		},
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("evidenceForToolMessage: %v", err)
	}
	var rollback *PlanEvidence
	for i := range items {
		if items[i].Kind == EvidenceKindRollback {
			rollback = &items[i]
			break
		}
	}
	if rollback == nil {
		t.Fatalf("rollback evidence missing: %+v", items)
	}
	if rollback.Status != EvidenceStatusFailed || rollback.Error != "workspace rollback conflict" || len(rollback.Paths) != 1 || rollback.Paths[0] != "notes.txt" {
		t.Fatalf("rollback evidence = %+v", rollback)
	}
	if rollback.ToolResultRef != "tool_result:run_parent:call_2" {
		t.Fatalf("rollback tool_result_ref = %q", rollback.ToolResultRef)
	}
}

func TestEvidenceForToolMessageDerivesFailedRollbackEvidenceFromNonJSONToolError(t *testing.T) {
	recordedAt := time.Now().UTC()
	items, err := evidenceForToolMessage(toolMessageEvidenceInput{
		Step:          PlanStep{ID: "s1"},
		RunID:         "run_parent",
		ToolName:      "rollback_workspace_checkpoint",
		ToolCallID:    "call_2",
		ArgumentsJSON: `{"checkpoint_id":"workspace_checkpoint_1"}`,
		Message: &planToolMessage{
			Content: `Tool call "rollback_workspace_checkpoint" failed: workspace rollback conflict: notes.txt`,
			Extra: map[string]any{
				"tool_result_ref":   "tool_result:run_parent:call_2",
				"tool_error":        true,
				"tool_error_reason": "workspace rollback conflict: notes.txt",
			},
		},
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("evidenceForToolMessage: %v", err)
	}
	var rollback *PlanEvidence
	for i := range items {
		if items[i].Kind == EvidenceKindRollback {
			rollback = &items[i]
			break
		}
	}
	if rollback == nil {
		t.Fatalf("rollback evidence missing: %+v", items)
	}
	if rollback.Status != EvidenceStatusFailed || rollback.Error != "workspace rollback conflict: notes.txt" {
		t.Fatalf("rollback evidence = %+v", rollback)
	}
	if rollback.ToolResultRef != "tool_result:run_parent:call_2" {
		t.Fatalf("rollback tool_result_ref = %q", rollback.ToolResultRef)
	}
}

func TestVerifierEvidenceFromPassedResultCountsForVerifierIntent(t *testing.T) {
	recordedAt := time.Now().UTC()
	evidence := verifierEvidenceFromResult("s1", "run_parent", &orchestration.VerificationResult{
		ChildRunID: "run_child_1",
		Verdict:    orchestration.VerificationVerdictPassed,
		Summary:    "all acceptance criteria passed",
	}, recordedAt)
	if evidence.Kind != EvidenceKindVerifier || evidence.Status != EvidenceStatusPassed || evidence.ChildRunID != "run_child_1" {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
	if evidence.SourceRunID != "run_parent" || !evidence.RecordedAt.Equal(recordedAt) {
		t.Fatalf("unexpected evidence metadata: %+v", evidence)
	}
	err := ensureVerificationIntentCoverage(PlanStep{
		ID: "s1",
		VerificationIntent: []VerificationIntent{{
			Kind: "verifier",
		}},
		Evidence: []PlanEvidence{evidence},
	})
	if err != nil {
		t.Fatalf("ensureVerificationIntentCoverage: %v", err)
	}
}

func TestVerifierEvidenceFromFailedResultPreservesFindings(t *testing.T) {
	evidence := verifierEvidenceFromResult("s1", "run_parent", &orchestration.VerificationResult{
		ChildRunID:       "run_child_1",
		Verdict:          orchestration.VerificationVerdictFailed,
		BlockingFindings: []string{"missing integration test", "openapi drift"},
	}, time.Now().UTC())
	if evidence.Status != EvidenceStatusFailed {
		t.Fatalf("status = %q, want failed", evidence.Status)
	}
	if evidence.Error != "missing integration test; openapi drift" {
		t.Fatalf("error = %q", evidence.Error)
	}
}

func TestVerifierEvidenceFromInconclusiveResultDoesNotCountAsCoverage(t *testing.T) {
	evidence := verifierEvidenceFromResult("s1", "run_parent", &orchestration.VerificationResult{
		ChildRunID:      "run_child_1",
		Verdict:         orchestration.VerificationVerdictInconclusive,
		MissingEvidence: []string{"tool result ref unavailable"},
	}, time.Now().UTC())
	if evidence.Status != EvidenceStatusRecorded {
		t.Fatalf("status = %q, want recorded", evidence.Status)
	}
	if evidence.Error != "tool result ref unavailable" {
		t.Fatalf("error = %q", evidence.Error)
	}
	err := ensureVerificationIntentCoverage(PlanStep{
		ID: "s1",
		VerificationIntent: []VerificationIntent{{
			Kind: "verifier",
		}},
		Evidence: []PlanEvidence{evidence},
	})
	if !errors.Is(err, ErrPlanStepVerificationGap) {
		t.Fatalf("error = %v, want ErrPlanStepVerificationGap", err)
	}
}

func TestVerifierEvidenceFromNilResultIsRecordedFailureEvidence(t *testing.T) {
	evidence := verifierEvidenceFromResult("s1", "run_parent", nil, time.Now().UTC())
	if evidence.Kind != EvidenceKindVerifier || evidence.Status != EvidenceStatusRecorded {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
	if evidence.Error != "verifier result is nil" {
		t.Fatalf("error = %q", evidence.Error)
	}
}
