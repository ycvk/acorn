package store

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/core"
)

// UpsertSessionSummary implements core.SessionSummaryStore.
// The session_summaries table was removed in the Phase 3 schema redesign
// (compact boundary not persisted); summaries are now file-backed memory.
func (s *Store) UpsertSessionSummary(ctx context.Context, summary core.SessionSummary) error {
	return errors.New("session summaries are not persisted")
}

// GetSessionSummary implements core.SessionSummaryStore.
func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*core.SessionSummary, error) {
	return nil, errors.New("session summaries are not persisted")
}
