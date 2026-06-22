package web

import (
	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/skills"
)

type SkillRequirementsDTO struct {
	Tools    []string `json:"tools,omitempty"`
	Toolsets []string `json:"toolsets,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Env      []string `json:"env,omitempty"`
}

type SkillSummaryDTO struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Version         string               `json:"version"`
	Category        string               `json:"category,omitempty"`
	Source          string               `json:"source"`
	Origin          skills.Origin        `json:"origin"`
	TaskPattern     string               `json:"task_pattern,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	PromotedFrom    string               `json:"promoted_from,omitempty"`
	Eligible        bool                 `json:"eligible"`
	Requirements    SkillRequirementsDTO `json:"requirements,omitempty"`
	DisabledReasons []string             `json:"disabled_reasons,omitempty"`
	CreatedByRunID  string               `json:"created_by_run_id,omitempty"`
	Replaces        []string             `json:"replaces,omitempty"`
}

type SkillDetailDTO struct {
	SkillSummaryDTO
	Path         string   `json:"path"`
	Instruction  string   `json:"instruction"`
	Scripts      []string `json:"scripts,omitempty"`
	Files        []string `json:"files,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Platforms    []string `json:"platforms,omitempty"`
	TriggerHints []string `json:"trigger_hints,omitempty"`
}

type SkillListResponse struct {
	Items []SkillSummaryDTO `json:"items"`
	Total int               `json:"total"`
}

type SkillEnvelope struct {
	Item SkillDetailDTO `json:"item"`
}

type SkillFileResponse struct {
	Item app.SkillFileView `json:"item"`
}

func skillSummaryDTOFromView(item skills.View) SkillSummaryDTO {
	return SkillSummaryDTO{
		ID:             item.ID,
		Name:           item.Name,
		Version:        item.Version,
		Category:       item.Category,
		Source:         item.Source,
		Origin:         item.Origin,
		TaskPattern:    item.TaskPattern,
		Summary:        item.Summary,
		PromotedFrom:   item.PromotedFrom,
		Eligible:       item.Eligible,
		Requirements:   skillRequirementsDTOFromDomain(item.Requires),
		CreatedByRunID: item.CreatedByRunID,
		Replaces:       append([]string(nil), item.Replaces...),
	}
}

func skillDetailDTOFromView(item skills.View) SkillDetailDTO {
	return SkillDetailDTO{
		SkillSummaryDTO: skillSummaryDTOFromView(item),
		Path:            item.Path,
		Instruction:     item.Instruction,
		Scripts:         append([]string(nil), item.Scripts...),
		Files:           append([]string(nil), item.Files...),
		Tags:            append([]string(nil), item.Tags...),
		Platforms:       append([]string(nil), item.Platforms...),
		TriggerHints:    append([]string(nil), item.TriggerHints...),
	}
}

func skillSummaryDTOsFromViews(items []skills.View) []SkillSummaryDTO {
	out := make([]SkillSummaryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, skillSummaryDTOFromView(item))
	}
	return out
}

func skillRequirementsDTOFromDomain(item skills.Requirements) SkillRequirementsDTO {
	return DefaultConverter.skillRequirementsDTOFromDomain(item)
}
