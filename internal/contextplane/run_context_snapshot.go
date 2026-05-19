package contextplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/workingstate"
)

type runContextAssembler struct {
	store             RunContextSnapshotStore
	checkpointService CheckpointService
}

type assembledRunContext struct {
	snapshot          *runtimehistory.RunContextSnapshot
	checkpointSection string
}

func (a runContextAssembler) Assemble(ctx context.Context, req AssembleRequest) (*assembledRunContext, error) {
	if strings.TrimSpace(req.Input) != "" {
		return a.createSnapshot(ctx, req, req.SelectedSkill, req.DecisionRecord)
	}
	if strings.TrimSpace(req.RunID) != "" {
		return a.loadSnapshot(ctx, req)
	}
	return &assembledRunContext{}, nil
}

func (a runContextAssembler) createSnapshot(ctx context.Context, req AssembleRequest, selectedSkill *SelectedSkill, record *decision.Record) (*assembledRunContext, error) {
	snapshot := runtimehistory.RunContextSnapshot{
		RunID:     req.RunID,
		CreatedAt: time.Now().UTC(),
	}
	var checkpointSection string
	if !isNilInterface(a.checkpointService) && strings.TrimSpace(req.SessionID) != "" {
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

	if selectedSkill != nil {
		snapshot.DecisionSkillID = selectedSkill.Skill.ID
	}
	if record != nil {
		snapshot.DecisionProfileHash = record.DecisionProfileHash
		snapshot.DecisionAction = string(record.Action)
		if strings.TrimSpace(record.SelectedSkillID) != "" {
			snapshot.DecisionSkillID = record.SelectedSkillID
		}
	}
	if strings.TrimSpace(req.RunID) != "" {
		if a.store == nil {
			return nil, fmt.Errorf("save run context snapshot: store is nil")
		}
		if err := a.store.SaveRunContextSnapshot(ctx, snapshot); err != nil {
			return nil, fmt.Errorf("save run context snapshot: %w", err)
		}
	}
	return &assembledRunContext{
		snapshot:          &snapshot,
		checkpointSection: checkpointSection,
	}, nil
}

func (a runContextAssembler) loadSnapshot(ctx context.Context, req AssembleRequest) (*assembledRunContext, error) {
	if a.store == nil {
		return nil, fmt.Errorf("load run context snapshot: store is nil")
	}
	snapshot, err := a.store.LoadRunContextSnapshot(ctx, req.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run context snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("run context snapshot missing for %s", req.RunID)
	}
	checkpointSection := workingstate.FormatForPrompt(&workingstate.Checkpoint{
		ThreadID:       req.SessionID,
		Content:        snapshot.WorkingCheckpointContent,
		RelatedSkillID: snapshot.WorkingCheckpointSkillID,
	})
	return &assembledRunContext{
		snapshot:          snapshot,
		checkpointSection: checkpointSection,
	}, nil
}
