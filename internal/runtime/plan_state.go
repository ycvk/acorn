package runtime

import (
	"fmt"
	"strings"
)

func findSingleInProgressPlanStep(plan *Plan) (int, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return -1, fmt.Errorf("active plan has no steps")
	}
	index := -1
	for i, step := range plan.Steps {
		if step.Status != PlanStepInProgress {
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

func findRunnablePlanStep(plan *Plan) (int, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return -1, fmt.Errorf("active plan has no steps")
	}
	inProgress := -1
	for i, step := range plan.Steps {
		if step.Status != PlanStepInProgress {
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
		if step.Status != PlanStepPending {
			continue
		}
		if planStepDependenciesCompleted(plan, step) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("active plan has no runnable pending step")
}

func planStepDependenciesCompleted(plan *Plan, step PlanStep) bool {
	if len(step.DependsOn) == 0 {
		return true
	}
	statusByID := make(map[string]PlanStepStatus, len(plan.Steps))
	for _, candidate := range plan.Steps {
		statusByID[candidate.ID] = candidate.Status
	}
	for _, dep := range step.DependsOn {
		if statusByID[strings.TrimSpace(dep)] != PlanStepCompleted {
			return false
		}
	}
	return true
}

func allPlanStepsTerminal(plan *Plan) bool {
	if plan == nil || len(plan.Steps) == 0 {
		return false
	}
	for _, step := range plan.Steps {
		if !planStepTerminal(step.Status) {
			return false
		}
	}
	return true
}

func planStepTerminal(status PlanStepStatus) bool {
	switch status {
	case PlanStepCompleted, PlanStepFailed, PlanStepSkipped:
		return true
	default:
		return false
	}
}
