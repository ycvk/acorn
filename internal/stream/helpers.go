package stream

import "github.com/ycvk/acorn/internal/model"

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

// StreamPlanFromDomain converts a model.Plan to a stream payload Plan.
func StreamPlanFromDomain(plan *model.Plan) *model.Plan {
	if plan == nil {
		return nil
	}
	return &model.Plan{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     ClonePlanSteps(plan.Steps),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

// StreamStepPayloadFromPlan creates a PlanStepPayload from a model.Plan and model.PlanStep.
func StreamStepPayloadFromPlan(plan *model.Plan, step model.PlanStep) PlanStepPayload {
	return PlanStepPayload{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Plan:      StreamPlanFromDomain(plan),
		Step:      ClonePlanStepPtr(step),
		UpdatedAt: plan.UpdatedAt,
	}
}

// PlanStepPayloadToMap flattens a PlanStepPayload into a map[string]any.
func PlanStepPayloadToMap(p PlanStepPayload) map[string]any {
	m := map[string]any{
		"plan_id":    p.PlanID,
		"session_id": p.SessionID,
		"run_id":     p.RunID,
		"updated_at": p.UpdatedAt,
	}
	if p.Plan != nil {
		m["plan"] = p.Plan
	}
	if p.Step != nil {
		m["step"] = p.Step
	}
	return m
}

// ClonePlanSteps deep-copies a slice of model.PlanStep.
func ClonePlanSteps(steps []model.PlanStep) []model.PlanStep {
	out := make([]model.PlanStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, ClonePlanStep(step))
	}
	return out
}

// ClonePlanStepPtr returns a pointer to a deep-copied model.PlanStep.
func ClonePlanStepPtr(step model.PlanStep) *model.PlanStep {
	return new(ClonePlanStep(step))
}

// ClonePlanStep deep-copies a single model.PlanStep.
func ClonePlanStep(step model.PlanStep) model.PlanStep {
	return model.PlanStep{
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

// ClonePlanRepoTargets deep-copies a slice of model.PlanRepoTarget.
func ClonePlanRepoTargets(items []model.PlanRepoTarget) []model.PlanRepoTarget {
	out := make([]model.PlanRepoTarget, 0, len(items))
	out = append(out, items...)
	return out
}

// CloneVerificationIntents deep-copies a slice of model.VerificationIntent.
func CloneVerificationIntents(items []model.VerificationIntent) []model.VerificationIntent {
	out := make([]model.VerificationIntent, 0, len(items))
	for _, item := range items {
		out = append(out, model.VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return out
}

// ClonePlanEvidence deep-copies a slice of model.PlanEvidence.
func ClonePlanEvidence(items []model.PlanEvidence) []model.PlanEvidence {
	out := make([]model.PlanEvidence, 0, len(items))
	out = append(out, items...)
	return out
}
