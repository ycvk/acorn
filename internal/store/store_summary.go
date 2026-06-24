package store

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/domain"
)

// UpsertSessionSummary implements domain.SessionSummaryStore.
// The session_summaries table was removed in the Phase 3 schema redesign
// (compact boundary not persisted); summaries are now file-backed memory.
func (s *Store) UpsertSessionSummary(ctx context.Context, summary domain.SessionSummary) error {
	return errors.New("session summaries are not persisted")
}

// GetSessionSummary implements domain.SessionSummaryStore.
func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*domain.SessionSummary, error) {
	return nil, errors.New("session summaries are not persisted")
}
