package web

import (
	"time"

	"github.com/ycvk/acorn/internal/model"
)

type PlanStepDTO struct {
	ID                 string                  `json:"id"`
	Action             string                  `json:"action"`
	Status             string                  `json:"status"`
	DependsOn          []string                `json:"depends_on,omitempty"`
	RepoTargets        []PlanRepoTargetDTO     `json:"repo_targets,omitempty"`
	VerificationIntent []VerificationIntentDTO `json:"verification_intent,omitempty"`
	Risk               string                  `json:"risk,omitempty"`
	ToolHints          []string                `json:"tool_hints,omitempty"`
	Evidence           []PlanEvidenceDTO       `json:"evidence,omitempty"`
}

type PlanRepoTargetDTO struct {
	Path       string `json:"path"`
	Symbol     string `json:"symbol,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"`
}

type VerificationIntentDTO struct {
	Kind    string   `json:"kind"`
	Command []string `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Reason  string   `json:"reason"`
}

type PlanEvidenceDTO struct {
	ID          string   `json:"id"`
	StepID      string   `json:"step_id"`
	Kind        string   `json:"kind"`
	Status      string   `json:"status"`
	Summary     string   `json:"summary"`
	ToolName    string   `json:"tool_name,omitempty"`
	Command     []string `json:"command,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	DiffRef     string   `json:"diff_ref,omitempty"`
	ChildRunID  string   `json:"child_run_id,omitempty"`
	Error       string   `json:"error,omitempty"`
	SourceRunID string   `json:"source_run_id,omitempty"`
	RecordedAt  string   `json:"recorded_at,omitempty"`
}

type PlanDTO struct {
	PlanID    string        `json:"plan_id"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Steps     []PlanStepDTO `json:"steps"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func planDTOFromRuntime(plan *model.Plan) *PlanDTO {
	if plan == nil {
		return nil
	}
	steps := make([]PlanStepDTO, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, PlanStepDTO{
			ID:                 step.ID,
			Action:             step.Action,
			Status:             string(step.Status),
			DependsOn:          append([]string(nil), step.DependsOn...),
			RepoTargets:        planRepoTargetDTOsFromRuntime(step.RepoTargets),
			VerificationIntent: verificationIntentDTOsFromRuntime(step.VerificationIntent),
			Risk:               string(step.Risk),
			ToolHints:          append([]string(nil), step.ToolHints...),
			Evidence:           planEvidenceDTOsFromRuntime(step.Evidence),
		})
	}
	return &PlanDTO{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     steps,
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

func planRepoTargetDTOsFromRuntime(items []model.PlanRepoTarget) []PlanRepoTargetDTO {
	return DefaultConverter.planRepoTargetDTOsFromRuntime(items)
}

func verificationIntentDTOsFromRuntime(items []model.VerificationIntent) []VerificationIntentDTO {
	return DefaultConverter.verificationIntentDTOsFromRuntime(items)
}

func planEvidenceDTOsFromRuntime(items []model.PlanEvidence) []PlanEvidenceDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]PlanEvidenceDTO, 0, len(items))
	for _, item := range items {
		recordedAt := ""
		if !item.RecordedAt.IsZero() {
			recordedAt = item.RecordedAt.Format(time.RFC3339)
		}
		result = append(result, PlanEvidenceDTO{
			ID:          item.ID,
			StepID:      item.StepID,
			Kind:        string(item.Kind),
			Status:      string(item.Status),
			Summary:     item.Summary,
			ToolName:    item.ToolName,
			Command:     append([]string(nil), item.Command...),
			Paths:       append([]string(nil), item.Paths...),
			DiffRef:     item.DiffRef,
			ChildRunID:  item.ChildRunID,
			Error:       item.Error,
			SourceRunID: item.SourceRunID,
			RecordedAt:  recordedAt,
		})
	}
	return result
}
