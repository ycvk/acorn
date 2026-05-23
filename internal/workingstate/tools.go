package workingstate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

type UpdateWorkingCheckpointInput struct {
	Content        string `json:"content" jsonschema:"required,description=Current session checkpoint content"`
	RelatedSkillID string `json:"related_skill_id" jsonschema:"description=Optional skill ID this checkpoint is anchored to"`
}

type CheckpointService interface {
	Get(ctx context.Context, threadID string) (*Checkpoint, error)
	Update(ctx context.Context, threadID, content, relatedSkillID string) (*Checkpoint, error)
	Clear(ctx context.Context, threadID string) error
}

func BuildWorkingCheckpointTools(checkpoints CheckpointService, sessionID string) ([]einotool.BaseTool, error) {
	trimmedSessionID := strings.TrimSpace(sessionID)

	items := make([]einotool.BaseTool, 0, 2)

	if checkpoints != nil && trimmedSessionID != "" {
		updateCheckpoint, err := toolutils.InferTool("update_working_checkpoint", "Update the current session working checkpoint. Use this for short-lived task focus, partial plans, and active debugging notes.", func(ctx context.Context, input UpdateWorkingCheckpointInput) (string, error) {
			checkpoint, err := checkpoints.Update(ctx, trimmedSessionID, input.Content, input.RelatedSkillID)
			if err != nil {
				return "", err
			}
			body, err := json.Marshal(checkpoint)
			if err != nil {
				return "", fmt.Errorf("marshal working checkpoint: %w", err)
			}
			return string(body), nil
		})
		if err != nil {
			return nil, fmt.Errorf("build update_working_checkpoint tool: %w", err)
		}
		items = append(items, updateCheckpoint)

		clearCheckpoint, err := toolutils.InferTool("clear_working_checkpoint", "Clear the current session working checkpoint after a task is fully completed or no longer relevant.", func(ctx context.Context, _ struct{}) (string, error) {
			if err := checkpoints.Clear(ctx, trimmedSessionID); err != nil {
				return "", err
			}
			return `{"cleared":true}`, nil
		})
		if err != nil {
			return nil, fmt.Errorf("build clear_working_checkpoint tool: %w", err)
		}
		items = append(items, clearCheckpoint)
	}

	return items, nil
}
