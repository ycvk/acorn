package store

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/domain"
)

// SaveSummary implements port.SummaryRepo. The session_summaries table was
// removed in the Phase 3 schema redesign (compact boundary not persisted);
// summaries are now file-backed memory. This returns an error so callers
// fail loudly if they attempt to persist summaries through the store.
func (s *Store) SaveSummary(ctx context.Context, sessionID, sourceRunID, runStatus, summary string) error {
	return fmt.Errorf("session summaries are not persisted: %w", ErrSessionNotFound)
}

// LoadSummary implements port.SummaryRepo. The session_summaries table was
// removed in the Phase 3 schema redesign; summaries are now file-backed
// memory. This returns a not-found error indicating no summary is stored.
func (s *Store) LoadSummary(ctx context.Context, sessionID string) (*domain.SessionSummary, error) {
	return nil, fmt.Errorf("session summaries are not persisted: %w", ErrSessionNotFound)
}
