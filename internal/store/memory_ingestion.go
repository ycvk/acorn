package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) MemorySourceCursor(ctx context.Context, name string) (string, error) {
	var cursor string
	err := s.db.QueryRowContext(ctx, `SELECT version FROM memory_source_cursors WHERE name=?`, name).Scan(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return cursor, err
}

func (s *Store) AdvanceMemorySourceCursor(ctx context.Context, name, expected, next string, sources []core.MemorySource) error {
	if name == "" || next == "" {
		return errors.New("memory source cursor requires name and next")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, `SELECT version FROM memory_source_cursors WHERE name=?`, name).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current != expected {
			return core.ErrMemoryConflict
		}
		for _, source := range sources {
			if err := registerMemorySource(ctx, tx, source); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO memory_source_cursors(name,version) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET version=excluded.version`, name, next)
		return err
	})
}

// LinkRunInputSources attaches scheduler provenance to the canonical input
// before model preparation. Excluded parents abort that preparation.
func (s *Store) LinkRunInputSources(ctx context.Context, runID string, parents []string) error {
	if len(parents) == 0 {
		return nil
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var inputID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM memory_sources WHERE run_id=? AND kind='message' AND speaker='system' ORDER BY recorded_at DESC LIMIT 1`, runID).Scan(&inputID); err != nil {
			return err
		}
		for _, parent := range uniqueMemoryStrings(parents) {
			if _, err := loadMemorySource(ctx, s.memoryConnection(tx), parent, true); err != nil {
				return err
			}
			var excluded bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memory_exclusions WHERE source_id=?)`, parent).Scan(&excluded); err != nil {
				return err
			}
			if excluded {
				return core.ErrMemoryExcluded
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_source_links(source_id,parent_id) VALUES(?,?)`, inputID, parent); err != nil {
				return err
			}
		}
		return nil
	})
}
