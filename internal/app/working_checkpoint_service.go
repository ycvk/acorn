package app

import (
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/workingstate"
)

type WorkingCheckpointService struct {
	service *workingstate.Service
}

func NewWorkingCheckpointService(service *workingstate.Service) (*WorkingCheckpointService, error) {
	if service == nil {
		return nil, errors.New("working checkpoint service is required")
	}
	return &WorkingCheckpointService{service: service}, nil
}

func (s *WorkingCheckpointService) Get(ctx context.Context, threadID string) (*WorkingCheckpointView, error) {
	checkpoint, err := s.service.Get(ctx, threadID)
	if err != nil || checkpoint == nil {
		return nil, err
	}
	return checkpointViewFromDomain(*checkpoint), nil
}

func (s *WorkingCheckpointService) Update(ctx context.Context, threadID, content, relatedSkillID string) (*WorkingCheckpointView, error) {
	checkpoint, err := s.service.Update(ctx, threadID, content, relatedSkillID)
	if err != nil || checkpoint == nil {
		return nil, err
	}
	return checkpointViewFromDomain(*checkpoint), nil
}

func (s *WorkingCheckpointService) Clear(ctx context.Context, threadID string) error {
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
