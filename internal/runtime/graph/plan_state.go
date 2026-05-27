package graph

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func FindSingleInProgressPlanStep(plan *model.Plan) (int, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return -1, fmt.Errorf("active plan has no steps")
	}
	index := -1
	for i, step := range plan.Steps {
		if step.Status != model.PlanStepInProgress {
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

func FindRunnablePlanStep(plan *model.Plan) (int, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return -1, fmt.Errorf("active plan has no steps")
	}
	inProgress := -1
	for i, step := range plan.Steps {
		if step.Status != model.PlanStepInProgress {
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
		if step.Status != model.PlanStepPending {
			continue
		}
		if PlanStepDependenciesCompleted(plan, step) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("active plan has no runnable pending step")
}

func PlanStepDependenciesCompleted(plan *model.Plan, step model.PlanStep) bool {
	if len(step.DependsOn) == 0 {
		return true
	}
	statusByID := make(map[string]model.PlanStepStatus, len(plan.Steps))
	for _, candidate := range plan.Steps {
		statusByID[candidate.ID] = candidate.Status
	}
	for _, dep := range step.DependsOn {
		if statusByID[strings.TrimSpace(dep)] != model.PlanStepCompleted {
			return false
		}
	}
	return true
}

func AllPlanStepsTerminal(plan *model.Plan) bool {
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

func PlanStepTerminal(status model.PlanStepStatus) bool {
	switch status {
	case model.PlanStepCompleted, model.PlanStepFailed, model.PlanStepSkipped:
		return true
	default:
		return false
	}
}

// FormatPlanSummary formats a plan into a short summary of completed steps.
func FormatPlanSummary(plan *model.Plan) string {
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	var summary string
	for _, step := range plan.Steps {
		if step.Status == model.PlanStepCompleted {
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
