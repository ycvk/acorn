package stream

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtime/api"
)

func compactInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// StreamPlanFromDomain converts an api.Plan to a stream StreamPlan.
func StreamPlanFromDomain(plan *api.Plan) *StreamPlan {
	if plan == nil {
		return nil
	}
	return &StreamPlan{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     ClonePlanSteps(plan.Steps),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

// StreamStepPayloadFromPlan creates a PlanStepPayload from an api.Plan and api.PlanStep.
func StreamStepPayloadFromPlan(plan *api.Plan, step api.PlanStep) PlanStepPayload {
	return PlanStepPayload{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Plan:      StreamPlanFromDomain(plan),
		Step:      ClonePlanStepPtr(step),
		UpdatedAt: plan.UpdatedAt,
	}
}

// ClonePlanSteps deep-copies a slice of api.PlanStep.
func ClonePlanSteps(steps []api.PlanStep) []api.PlanStep {
	out := make([]api.PlanStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, ClonePlanStep(step))
	}
	return out
}

// ClonePlanStepPtr returns a pointer to a deep-copied api.PlanStep.
func ClonePlanStepPtr(step api.PlanStep) *api.PlanStep {
	return new(ClonePlanStep(step))
}

// ClonePlanStep deep-copies a single api.PlanStep.
func ClonePlanStep(step api.PlanStep) api.PlanStep {
	return api.PlanStep{
		ID:                 step.ID,
		Action:             step.Action,
		Status:             step.Status,
		DependsOn:          append([]string(nil), step.DependsOn...),
		RepoTargets:        ClonePlanRepoTargets(step.RepoTargets),
		VerificationIntent: CloneVerificationIntents(step.VerificationIntent),
		Risk:               step.Risk,
		ToolHints:          append([]string(nil), step.ToolHints...),
		Evidence:           ClonePlanEvidence(step.Evidence),
	}
}

// ClonePlanRepoTargets deep-copies a slice of api.PlanRepoTarget.
func ClonePlanRepoTargets(items []api.PlanRepoTarget) []api.PlanRepoTarget {
	out := make([]api.PlanRepoTarget, 0, len(items))
	out = append(out, items...)
	return out
}

// CloneVerificationIntents deep-copies a slice of api.VerificationIntent.
func CloneVerificationIntents(items []api.VerificationIntent) []api.VerificationIntent {
	out := make([]api.VerificationIntent, 0, len(items))
	for _, item := range items {
		out = append(out, api.VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return out
}

// ClonePlanEvidence deep-copies a slice of api.PlanEvidence.
func ClonePlanEvidence(items []api.PlanEvidence) []api.PlanEvidence {
	out := make([]api.PlanEvidence, 0, len(items))
	out = append(out, items...)
	return out
}

// FormatPlanSummary formats a plan into a short summary of completed steps.
func FormatPlanSummary(plan *api.Plan) string {
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	var summary string
	for _, step := range plan.Steps {
		if step.Status == api.PlanStepCompleted {
			if summary != "" {
				summary += "\n"
			}
			summary += step.Action
		}
	}
	if summary == "" {
		return ""
	}
	return summary
}

// AllPlanStepsTerminal reports whether every step in the plan is in a terminal state.
func AllPlanStepsTerminal(plan *api.Plan) bool {
	if plan == nil || len(plan.Steps) == 0 {
		return false
	}
	for _, step := range plan.Steps {
		if !PlanStepTerminal(step.Status) {
			return false
		}
	}
	return true
}

// PlanStepTerminal reports whether the given step status is terminal.
func PlanStepTerminal(status api.PlanStepStatus) bool {
	switch status {
	case api.PlanStepCompleted, api.PlanStepFailed, api.PlanStepSkipped:
		return true
	default:
		return false
	}
}

// FindSingleInProgressPlanStep returns the index of the single in-progress step.
func FindSingleInProgressPlanStep(plan *api.Plan) (int, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return -1, fmt.Errorf("active plan has no steps")
	}
	index := -1
	for i, step := range plan.Steps {
		if step.Status != api.PlanStepInProgress {
			continue
		}
		if index >= 0 {
			return -1, fmt.Errorf("active plan has multiple in_progress steps")
		}
		index = i
	}
	if index < 0 {
		return -1, fmt.Errorf("active plan has no in_progress step")
	}
	return index, nil
}

// FindRunnablePlanStep returns the index of the first runnable plan step.
func FindRunnablePlanStep(plan *api.Plan) (int, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return -1, fmt.Errorf("active plan has no steps")
	}
	inProgress := -1
	for i, step := range plan.Steps {
		if step.Status != api.PlanStepInProgress {
			continue
		}
		if inProgress >= 0 {
			return -1, fmt.Errorf("active plan has multiple in_progress steps")
		}
		inProgress = i
	}
	if inProgress >= 0 {
		return inProgress, nil
	}
	for i, step := range plan.Steps {
		if step.Status != api.PlanStepPending {
			continue
		}
		if PlanStepDependenciesCompleted(plan, step) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("active plan has no runnable pending step")
}

// PlanStepDependenciesCompleted reports whether all dependencies of the given step are completed.
func PlanStepDependenciesCompleted(plan *api.Plan, step api.PlanStep) bool {
	if len(step.DependsOn) == 0 {
		return true
	}
	statusByID := make(map[string]api.PlanStepStatus, len(plan.Steps))
	for _, candidate := range plan.Steps {
		statusByID[candidate.ID] = candidate.Status
	}
	for _, dep := range step.DependsOn {
		if statusByID[strings.TrimSpace(dep)] != api.PlanStepCompleted {
			return false
		}
	}
	return true
}
