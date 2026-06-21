package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
)

func verifierEvidenceFromResult(stepID, parentRunID string, result *orchestration.VerificationResult, recordedAt time.Time) model.PlanEvidence {
	if result == nil {
		return verifierNilResultEvidence(stepID, parentRunID, recordedAt)
	}
	status, errText := verifierResultStatus(result)
	summary := verifierResultSummary(result)
	return model.PlanEvidence{
		ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        model.EvidenceKindVerifier,
		Status:      status,
		Summary:     summary,
		ChildRunID:  strings.TrimSpace(result.ChildRunID),
		Error:       errText,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func verifierNilResultEvidence(stepID, parentRunID string, recordedAt time.Time) model.PlanEvidence {
	reason := "verifier result is nil"
	return model.PlanEvidence{
		ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        model.EvidenceKindVerifier,
		Status:      model.EvidenceStatusRecorded,
		Summary:     reason,
		Error:       reason,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func verifierResultStatus(result *orchestration.VerificationResult) (model.EvidenceStatus, string) {
	status := model.EvidenceStatusRecorded
	errText := ""
	switch result.Verdict {
	case orchestration.VerificationVerdictPassed:
		status = model.EvidenceStatusPassed
	case orchestration.VerificationVerdictFailed:
		status = model.EvidenceStatusFailed
		errText = strings.Join(trimmedNonEmptyStrings(result.BlockingFindings), "; ")
		if errText == "" {
			errText = "verifier failed"
		}
	case orchestration.VerificationVerdictInconclusive:
		errText = strings.Join(trimmedNonEmptyStrings(result.MissingEvidence), "; ")
		if errText == "" {
			errText = "verifier inconclusive"
		}
	default:
		errText = fmt.Sprintf("verifier verdict %q is inconclusive", strings.TrimSpace(string(result.Verdict)))
	}
	return status, errText
}

func verifierResultSummary(result *orchestration.VerificationResult) string {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = fmt.Sprintf("verifier verdict: %s", strings.TrimSpace(string(result.Verdict)))
	}
	return summary
}

var (
	ErrRiskyToolRequiresPlan   = errors.New("risky tool execution requires an active persisted plan")
	ErrPlanStepVerificationGap = errors.New("plan step requires recorded verification before completion")
)

func enforceRiskyToolPlan(ctx context.Context, planStore runtimeapi.PlanStore, spec tooling.ToolSpec) (string, string, error) {
	if spec.PlanPolicy != tooling.PlanPolicyRequireActivePlan {
		return "", "", nil
	}
	if planStore == nil {
		return "", "", errors.New("plan enforcement store is not available")
	}
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	if sessionID == "" {
		return "", "", fmt.Errorf("%w: session_id not available for %s", ErrRiskyToolRequiresPlan, spec.Name)
	}
	plan, err := planStore.LoadPlan(ctx, sessionID)
	if err != nil {
		return riskyLoadPlanError(spec, err)
	}
	stepIndex, err := graph.FindSingleInProgressPlanStep(plan)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrRiskyToolRequiresPlan, err)
	}
	return sessionID, plan.Steps[stepIndex].ID, nil
}

func riskyLoadPlanError(spec tooling.ToolSpec, err error) (string, string, error) {
	if errors.Is(err, store.ErrPlanNotFound) {
		return "", "", fmt.Errorf("%w: active plan not available before %s", ErrRiskyToolRequiresPlan, spec.Name)
	}
	return "", "", fmt.Errorf("load active plan for %s: %w", spec.Name, err)
}
