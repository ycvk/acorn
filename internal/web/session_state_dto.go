package web

import (
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
)

type SelectedSkillDTO struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Source       string               `json:"source,omitempty"`
	Origin       string               `json:"origin,omitempty"`
	TaskPattern  string               `json:"task_pattern,omitempty"`
	Summary      string               `json:"summary,omitempty"`
	PromotedFrom string               `json:"promoted_from,omitempty"`
	Requirements SkillRequirementsDTO `json:"requirements,omitempty"`
	Score        int                  `json:"score,omitempty"`
	MatchedTerms []string             `json:"matched_terms,omitempty"`
}

type RunDecisionDTO struct {
	RunID               string    `json:"run_id"`
	Action              string    `json:"action"`
	Intent              string    `json:"intent,omitempty"`
	SelectedSkillID     string    `json:"selected_skill_id,omitempty"`
	DecisionReason      string    `json:"decision_reason,omitempty"`
	DecisionProfileHash string    `json:"decision_profile_hash,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

func summaryText(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.Summary
}

func summaryStatus(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.RunStatus
}

func summarySourceRunID(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.SourceRunID
}

func summaryUpdatedAt(summary *runtimehistory.SessionSummary) *time.Time {
	if summary == nil {
		return nil
	}
	return &summary.UpdatedAt
}

func runDecisionDTOFromDomain(record *decision.Record) *RunDecisionDTO {
	if record == nil {
		return nil
	}
	return &RunDecisionDTO{
		RunID:               record.RunID,
		Action:              string(record.Action),
		Intent:              record.Intent,
		SelectedSkillID:     record.SelectedSkillID,
		DecisionReason:      record.DecisionReason,
		DecisionProfileHash: record.DecisionProfileHash,
		CreatedAt:           record.CreatedAt,
	}
}

func selectedSkillDTOFromRuntime(skill *runtime.SelectedSkill) *SelectedSkillDTO {
	if skill == nil {
		return nil
	}
	return &SelectedSkillDTO{
		ID:           skill.Skill.ID,
		Name:         skill.Skill.Name,
		Source:       skill.Skill.Source,
		Origin:       string(skill.Skill.Origin),
		TaskPattern:  skill.Skill.TaskPattern,
		Summary:      skill.Skill.Summary,
		PromotedFrom: skill.Skill.PromotedFrom,
		Requirements: skillRequirementsDTOFromDomain(skill.Skill.Requires),
		Score:        skill.Score,
		MatchedTerms: append([]string(nil), skill.MatchedTerms...),
	}
}
