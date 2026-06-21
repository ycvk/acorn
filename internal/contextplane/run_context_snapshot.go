package contextplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/workingstate"
)

type runContextAssembler struct {
	checkpointService CheckpointService
}

type assembledRunContext struct {
	snapshot          *model.RunContextSnapshot
	checkpointSection string
}

func (a runContextAssembler) Assemble(ctx context.Context, req AssembleRequest) (*assembledRunContext, error) {
	if strings.TrimSpace(req.Input) != "" {
		return a.createSnapshot(ctx, req, req.SelectedSkill)
	}
	if strings.TrimSpace(req.RunID) != "" {
		return &assembledRunContext{}, nil
	}
	return &assembledRunContext{}, nil
}

func (a runContextAssembler) createSnapshot(ctx context.Context, req AssembleRequest, selectedSkill *SelectedSkill) (*assembledRunContext, error) {
	snapshot := model.RunContextSnapshot{
		RunID:     req.RunID,
		CreatedAt: time.Now().UTC(),
	}
	var checkpointSection string
	if !IsNilInterface(a.checkpointService) && strings.TrimSpace(req.SessionID) != "" {
		checkpoint, err := a.checkpointService.Get(ctx, req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("load working checkpoint: %w", err)
		}
		if checkpoint != nil {
			snapshot.WorkingCheckpointContent = checkpoint.Content
			snapshot.WorkingCheckpointSkillID = checkpoint.RelatedSkillID
			checkpointSection = workingstate.FormatForPrompt(checkpoint)
		}
	}

	return &assembledRunContext{
		snapshot:          &snapshot,
		checkpointSection: checkpointSection,
	}, nil
}

