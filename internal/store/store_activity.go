package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) SaveContextSnapshot(ctx context.Context, hash, content string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO context_snapshots(hash, content, created_at) VALUES(?, ?, ?)`,
		hash, content, formatTimestamp(time.Now().UTC())); err != nil {
		return fmt.Errorf("save context snapshot %s: %w", hash, err)
	}
	return nil
}

func (s *Store) CountWakesSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE kind = ? AND created_at >= ?`,
		core.EventWakeFired, formatTimestamp(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count wakes: %w", err)
	}
	return n, nil
}
