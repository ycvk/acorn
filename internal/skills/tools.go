package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

type ToolListInput struct{}
type ToolViewInput struct {
	ID string `json:"id" jsonschema:"required,description=Skill ID to inspect"`
}
type ToolCreateInput struct {
	ID           string            `json:"id" jsonschema:"required,description=New skill ID"`
	Name         string            `json:"name" jsonschema:"required,description=Skill display name"`
	Version      string            `json:"version,omitempty" jsonschema:"description=Skill version"`
	Category     string            `json:"category,omitempty" jsonschema:"description=Skill category"`
	Summary      string            `json:"summary,omitempty" jsonschema:"description=Short skill summary"`
	PromotedFrom string            `json:"promoted_from,omitempty" jsonschema:"description=Source skill or procedure ref"`
	Origin       Origin            `json:"origin,omitempty" jsonschema:"description=human or distilled"`
	TaskPattern  string            `json:"task_pattern,omitempty" jsonschema:"description=Task pattern for distilled skills"`
	Instruction  string            `json:"instruction" jsonschema:"required,description=Markdown instruction body"`
	Tags         []string          `json:"tags,omitempty" jsonschema:"description=Skill tags"`
	Platforms    []string          `json:"platforms,omitempty" jsonschema:"description=Supported platforms"`
	TriggerHints []string          `json:"trigger_hints,omitempty" jsonschema:"description=Selection trigger hints"`
	Requires     Requirements      `json:"requirements,omitempty" jsonschema:"description=Required tools, toolsets, binaries, and env vars"`
	EvidenceRefs []string          `json:"evidence_refs,omitempty" jsonschema:"description=Evidence refs supporting verified lifecycle status"`
	Files        map[string]string `json:"files,omitempty" jsonschema:"description=Additional relative files to create inside the skill package"`
}

func BuildAgentTools(loader *Loader) ([]einotool.BaseTool, error) {
	if loader == nil {
		return nil, errors.New("skill loader is required")
	}
	listTool, err := toolutils.InferTool("skill_list", "List available filesystem skills with eligibility metadata.", func(ctx context.Context, _ ToolListInput) (string, error) {
		scan, err := loader.ScanSkills(ctx)
		if err != nil {
			return "", err
		}
		body, err := json.Marshal(scan)
		if err != nil {
			return "", fmt.Errorf("marshal skills: %w", err)
		}
		return string(body), nil
	})
	if err != nil {
		return nil, fmt.Errorf("build skill_list tool: %w", err)
	}
	viewTool, err := toolutils.InferTool("skill_view", "Read one skill package, including SKILL.md content and supporting file list.", func(ctx context.Context, input ToolViewInput) (string, error) {
		trimmedID := strings.TrimSpace(input.ID)
		if trimmedID == "" {
			return "", errors.New("id is required")
		}
		scan, err := loader.ScanSkills(ctx)
		if err != nil {
			return "", err
		}
		for _, item := range scan.Skills {
			if item.ID != trimmedID {
				continue
			}
			body, err := json.Marshal(item)
			if err != nil {
				return "", fmt.Errorf("marshal skill %s: %w", trimmedID, err)
			}
			return string(body), nil
		}
		return "", fmt.Errorf("%w: %s", ErrNotFound, trimmedID)
	})
	if err != nil {
		return nil, fmt.Errorf("build skill_view tool: %w", err)
	}
	createTool, err := toolutils.InferTool("skill_create", "Create a generated filesystem skill package under Acorn storage. Use this with the built-in skill.creator workflow; new skills default to draft unless evidence-backed.", func(ctx context.Context, input ToolCreateInput) (string, error) {
		spec, err := loader.CreateSkill(ctx, CreateInput{
			ID:           input.ID,
			Name:         input.Name,
			Version:      input.Version,
			Category:     input.Category,
			Summary:      input.Summary,
			PromotedFrom: input.PromotedFrom,
			Origin:       input.Origin,
			TaskPattern:  input.TaskPattern,
			Instruction:  input.Instruction,
			Tags:         append([]string(nil), input.Tags...),
			Platforms:    append([]string(nil), input.Platforms...),
			TriggerHints: append([]string(nil), input.TriggerHints...),
			Requires:     CopyRequirements(input.Requires),
			EvidenceRefs: append([]string(nil), input.EvidenceRefs...),
		})
		if err != nil {
			return "", err
		}
		for rel, content := range input.Files {
			if err := loader.WriteSkillFile(ctx, spec.ID, rel, content); err != nil {
				return "", err
			}
		}
		scan, err := loader.ScanSkills(ctx)
		if err != nil {
			return "", err
		}
		for _, item := range scan.Skills {
			if item.ID != spec.ID {
				continue
			}
			body, err := json.Marshal(item)
			if err != nil {
				return "", fmt.Errorf("marshal skill %s: %w", spec.ID, err)
			}
			return string(body), nil
		}
		return "", fmt.Errorf("%w: %s", ErrNotFound, spec.ID)
	})
	if err != nil {
		return nil, fmt.Errorf("build skill_create tool: %w", err)
	}
	return []einotool.BaseTool{listTool, viewTool, createTool}, nil
}
