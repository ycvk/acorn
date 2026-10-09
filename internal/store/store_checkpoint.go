package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) LoadCheckpoint(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM agent_checkpoints WHERE checkpoint_id = ?`, checkpointID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load checkpoint %s: %w", checkpointID, err)
	}
	return data, true, nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, checkpointID string, data []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_checkpoints(checkpoint_id, data, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(checkpoint_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		checkpointID, data, formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("save checkpoint %s: %w", checkpointID, err)
	}
	return nil
}

func (s *Store) DeleteCheckpoint(ctx context.Context, checkpointID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_checkpoints WHERE checkpoint_id = ?`, checkpointID); err != nil {
		return fmt.Errorf("delete checkpoint %s: %w", checkpointID, err)
	}
	return nil
}
