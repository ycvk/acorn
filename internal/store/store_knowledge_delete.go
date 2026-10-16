package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// KnowledgeMemorySourceIDs lists the memory sources of every revision of the
// note at path, including revisions of a note deleted earlier at that path.
func (s *Store) KnowledgeMemorySourceIDs(ctx context.Context, path string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM memory_sources WHERE kind = 'knowledge' AND object_id = ? ORDER BY id`, path)
	if err != nil {
		return nil, fmt.Errorf("list knowledge memory sources: %w", err)
	}
	return memoryStringRows(rows)
}

// DeleteKnowledgeNote removes the note at path with its revisions and search
// entry. It fails with core.ErrKnowledgeConflict when the note moved past
// revision, so a write that lands after the caller forgot the read revisions
// keeps the note. The note's memory sources stay, so the exclusions that
// forget them keep holding.
func (s *Store) DeleteKnowledgeNote(ctx context.Context, path string, revision int64) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		current, err := loadKnowledgeNote(ctx, tx, path)
		if err != nil {
			return err
		}
		if current.Revision != revision {
			return fmt.Errorf("%w: %s is at revision %d, not %d", core.ErrKnowledgeConflict, path, current.Revision, revision)
		}
		for _, stmt := range []string{
			`DELETE FROM knowledge_notes_fts WHERE path = ?`,
			`DELETE FROM knowledge_revisions WHERE path = ?`,
			`DELETE FROM knowledge_notes WHERE path = ?`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, path); err != nil {
				return fmt.Errorf("delete knowledge note %s: %w", path, err)
			}
		}
		return nil
	})
}
