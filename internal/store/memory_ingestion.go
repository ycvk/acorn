package store

import (
	"context"
	"database/sql"

	"github.com/ycvk/acorn/internal/core"
)

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
			if _, err := loadMemorySource(ctx, tx, parent, true); err != nil {
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
