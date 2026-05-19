package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/workingstate"
)

func (s *Store) GetWorkingCheckpoint(ctx context.Context, threadID string) (*workingstate.Checkpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, content, related_skill_id, updated_at
		 FROM working_checkpoints WHERE session_id = ?`,
		strings.TrimSpace(threadID),
	)
	var (
		checkpoint workingstate.Checkpoint
		updatedAt  string
	)
	if err := row.Scan(&checkpoint.ThreadID, &checkpoint.Content, &checkpoint.RelatedSkillID, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get working checkpoint: %w", err)
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, updatedAt, "working_checkpoint.updated_at")
	if err != nil {
		return nil, err
	}
	checkpoint.UpdatedAt = parsed
	return &checkpoint, nil
}

func (s *Store) UpsertWorkingCheckpoint(ctx context.Context, checkpoint workingstate.Checkpoint) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO working_checkpoints(session_id, content, related_skill_id, updated_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     content = excluded.content,
		     related_skill_id = excluded.related_skill_id,
		     updated_at = excluded.updated_at`,
		checkpoint.ThreadID,
		checkpoint.Content,
		checkpoint.RelatedSkillID,
		formatTimestamp(checkpoint.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert working checkpoint: %w", err)
	}
	return nil
}

func (s *Store) DeleteWorkingCheckpoint(ctx context.Context, threadID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM working_checkpoints WHERE session_id = ?`, strings.TrimSpace(threadID)); err != nil {
		return fmt.Errorf("delete working checkpoint: %w", err)
	}
	return nil
}
