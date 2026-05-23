package graph

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtime/api"
)

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

func PlanStepTerminal(status api.PlanStepStatus) bool {
	switch status {
	case api.PlanStepCompleted, api.PlanStepFailed, api.PlanStepSkipped:
		return true
	default:
		return false
	}
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
	if summary != "" {
		return summary
	}
	return "Plan finished."
}
