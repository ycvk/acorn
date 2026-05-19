package runtime

import "time"

type PlanStepPayload struct {
	PlanID    string      `json:"plan_id"`
	SessionID string      `json:"session_id"`
	RunID     string      `json:"run_id"`
	Plan      *StreamPlan `json:"plan"`
	Step      *PlanStep   `json:"step"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type PlanStepStartedPayload struct {
	PlanStepPayload
}

func (p *PlanStepStartedPayload) StreamKind() StreamItemKind { return StreamKindStepStarted }

type PlanStepCompletedPayload struct {
	PlanStepPayload
}

func (p *PlanStepCompletedPayload) StreamKind() StreamItemKind { return StreamKindStepCompleted }

type PlanStepFailedPayload struct {
	PlanStepPayload
	Error string `json:"error,omitempty"`
}

func (p *PlanStepFailedPayload) StreamKind() StreamItemKind { return StreamKindStepFailed }

func streamPlanFromDomain(plan *Plan) *StreamPlan {
	if plan == nil {
		return nil
	}
	return &StreamPlan{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     clonePlanSteps(plan.Steps),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

func streamStepPayloadFromPlan(plan *Plan, step PlanStep) PlanStepPayload {
	return PlanStepPayload{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Plan:      streamPlanFromDomain(plan),
		Step:      clonePlanStepPtr(step),
		UpdatedAt: plan.UpdatedAt,
	}
}

func clonePlanSteps(steps []PlanStep) []PlanStep {
	out := make([]PlanStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, clonePlanStep(step))
	}
	return out
}

func clonePlanStepPtr(step PlanStep) *PlanStep {
	return new(clonePlanStep(step))
}

func clonePlanStep(step PlanStep) PlanStep {
	return PlanStep{
		ID:                 step.ID,
		Action:             step.Action,
		Status:             step.Status,
		DependsOn:          append([]string(nil), step.DependsOn...),
		RepoTargets:        clonePlanRepoTargets(step.RepoTargets),
		VerificationIntent: cloneVerificationIntents(step.VerificationIntent),
		Risk:               step.Risk,
		ToolHints:          append([]string(nil), step.ToolHints...),
		Evidence:           clonePlanEvidence(step.Evidence),
	}
}

func clonePlanRepoTargets(items []PlanRepoTarget) []PlanRepoTarget {
	out := make([]PlanRepoTarget, 0, len(items))
	out = append(out, items...)
	return out
}

func cloneVerificationIntents(items []VerificationIntent) []VerificationIntent {
	out := make([]VerificationIntent, 0, len(items))
	for _, item := range items {
		out = append(out, VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return out
}

func clonePlanEvidence(items []PlanEvidence) []PlanEvidence {
	out := make([]PlanEvidence, 0, len(items))
	out = append(out, items...)
	return out
}
