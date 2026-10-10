package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RecoverCommitmentClaims releases claims whose run never reached the binding
// step. A late starter then fails its state check before it can invoke a model.
func (s *Store) RecoverCommitmentClaims(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return errors.New("recover commitment claims requires now")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		cutoff := formatTimestamp(now.Add(-5 * time.Minute))
		if _, err := tx.ExecContext(ctx, `UPDATE commitments SET state='scheduled',wake_at=?,updated_at=? WHERE state='due' AND id IN(SELECT commitment_id FROM commitment_occurrences WHERE state='claimed' AND updated_at<=?)`, formatTimestamp(now), formatTimestamp(now), cutoff); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE commitment_occurrences SET state='start_failed',updated_at=? WHERE state='claimed' AND updated_at<=?`, formatTimestamp(now), cutoff)
		return err
	})
}
