package store

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/domain"
)

// UpsertSessionSummary implements domain.SessionSummaryStore.
// The session_summaries table was removed in the Phase 3 schema redesign
// (compact boundary not persisted); summaries are now file-backed memory.
func (s *Store) UpsertSessionSummary(ctx context.Context, summary domain.SessionSummary) error {
	return fmt.Errorf("session summaries are not persisted: %w", ErrSessionNotFound)
}

// GetSessionSummary implements domain.SessionSummaryStore.
func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*domain.SessionSummary, error) {
	return nil, fmt.Errorf("session summaries are not persisted: %w", ErrSessionNotFound)
}
