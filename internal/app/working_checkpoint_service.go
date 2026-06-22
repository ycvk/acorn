package app

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/workingstate"
)

// WorkingCheckpointService wraps the working-state domain service for the
// container. When the underlying store is nil — the current state after the
// architecture refactor dropped the working_checkpoints table — the service
// degrades to a no-op so container startup and the web routes that require a
// non-nil checkpoint service keep functioning (Get returns nothing, Update/Clear
// are silently dropped). This keeps the checkpoint surface wired without
// persisting until a store is reintroduced.
type WorkingCheckpointService struct {
	service *workingstate.Service
}

func NewWorkingCheckpointService(service *workingstate.Service) (*WorkingCheckpointService, error) {
	return &WorkingCheckpointService{service: service}, nil
}

func (s *WorkingCheckpointService) Get(ctx context.Context, threadID string) (*WorkingCheckpointView, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	checkpoint, err := s.service.Get(ctx, threadID)
	if err != nil || checkpoint == nil {
		return nil, err
	}
	return checkpointViewFromDomain(*checkpoint), nil
}

func (s *WorkingCheckpointService) Update(ctx context.Context, threadID, content, relatedSkillID string) (*WorkingCheckpointView, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	checkpoint, err := s.service.Update(ctx, threadID, content, relatedSkillID)
	if err != nil || checkpoint == nil {
		return nil, err
	}
	return checkpointViewFromDomain(*checkpoint), nil
}

func (s *WorkingCheckpointService) Clear(ctx context.Context, threadID string) error {
	if s == nil || s.service == nil {
		return nil
	}
	return s.service.Clear(ctx, threadID)
}

type WorkingCheckpointView struct {
	ThreadID       string    `json:"thread_id"`
	Content        string    `json:"content"`
	RelatedSkillID string    `json:"related_skill_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func checkpointViewFromDomain(checkpoint workingstate.Checkpoint) *WorkingCheckpointView {
	return &WorkingCheckpointView{
		ThreadID:       checkpoint.ThreadID,
		Content:        checkpoint.Content,
		RelatedSkillID: checkpoint.RelatedSkillID,
		UpdatedAt:      checkpoint.UpdatedAt,
	}
}
