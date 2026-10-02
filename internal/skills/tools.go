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
	ID string `json:"id" jsonschema:"required" jsonschema_description:"Skill ID to inspect"`
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
	return []einotool.BaseTool{listTool, viewTool}, nil
}
