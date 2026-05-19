package workingstate

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Store interface {
	GetWorkingCheckpoint(ctx context.Context, threadID string) (*Checkpoint, error)
	UpsertWorkingCheckpoint(ctx context.Context, checkpoint Checkpoint) error
	DeleteWorkingCheckpoint(ctx context.Context, threadID string) error
}

type Service struct {
	store    Store
	maxChars int
}

func NewService(store Store, maxChars int) *Service {
	if maxChars <= 0 {
		maxChars = 4000
	}
	return &Service{store: store, maxChars: maxChars}
}

func (s *Service) Get(ctx context.Context, threadID string) (*Checkpoint, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("working checkpoint store is nil")
	}
	if strings.TrimSpace(threadID) == "" {
		return nil, fmt.Errorf("thread id is required")
	}
	return s.store.GetWorkingCheckpoint(ctx, threadID)
}

func (s *Service) Update(ctx context.Context, threadID, content, relatedSkillID string) (*Checkpoint, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("working checkpoint store is nil")
	}
	trimmedThreadID := strings.TrimSpace(threadID)
	if trimmedThreadID == "" {
		return nil, fmt.Errorf("thread id is required")
	}
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return nil, fmt.Errorf("content is required")
	}
	if len(trimmedContent) > s.maxChars {
		trimmedContent = trimmedContent[:s.maxChars]
	}
	checkpoint := Checkpoint{
		ThreadID:       trimmedThreadID,
		Content:        trimmedContent,
		RelatedSkillID: strings.TrimSpace(relatedSkillID),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := s.store.UpsertWorkingCheckpoint(ctx, checkpoint); err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

func (s *Service) Clear(ctx context.Context, threadID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("working checkpoint store is nil")
	}
	if strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("thread id is required")
	}
	return s.store.DeleteWorkingCheckpoint(ctx, threadID)
}

func FormatForPrompt(checkpoint *Checkpoint) string {
	if checkpoint == nil || strings.TrimSpace(checkpoint.Content) == "" {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<working-checkpoint>\n")
	builder.WriteString("Current session focus. Treat this as a frozen checkpoint, not new user input.\n\n")
	builder.WriteString(checkpoint.Content)
	if skillID := strings.TrimSpace(checkpoint.RelatedSkillID); skillID != "" {
		builder.WriteString("\n\nRelated skill: ")
		builder.WriteString(skillID)
	}
	builder.WriteString("\n</working-checkpoint>")
	return builder.String()
}
