package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func (s *Store) SaveRunContextSnapshot(ctx context.Context, snapshot runtimehistory.RunContextSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_context_snapshots(run_id, working_checkpoint_content, working_checkpoint_skill_id, decision_profile_hash, decision_action, decision_skill_id, created_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(run_id) DO UPDATE SET
			     working_checkpoint_content = excluded.working_checkpoint_content,
			     working_checkpoint_skill_id = excluded.working_checkpoint_skill_id,
			     decision_profile_hash = excluded.decision_profile_hash,
			     decision_action = excluded.decision_action,
			     decision_skill_id = excluded.decision_skill_id,
		     created_at = excluded.created_at`,
		snapshot.RunID,
		snapshot.WorkingCheckpointContent,
		snapshot.WorkingCheckpointSkillID,
		snapshot.DecisionProfileHash,
		snapshot.DecisionAction,
		snapshot.DecisionSkillID,
		formatTimestamp(snapshot.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save run context snapshot: %w", err)
	}
	return nil
}

func (s *Store) LoadRunContextSnapshot(ctx context.Context, runID string) (*runtimehistory.RunContextSnapshot, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, working_checkpoint_content, working_checkpoint_skill_id, decision_profile_hash, decision_action, decision_skill_id, created_at
			 FROM run_context_snapshots WHERE run_id = ?`,
		runID,
	)
	var (
		snapshot  runtimehistory.RunContextSnapshot
		createdAt string
	)
	if err := row.Scan(
		&snapshot.RunID,
		&snapshot.WorkingCheckpointContent,
		&snapshot.WorkingCheckpointSkillID,
		&snapshot.DecisionProfileHash,
		&snapshot.DecisionAction,
		&snapshot.DecisionSkillID,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load run context snapshot: %w", err)
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "run_context_snapshot.created_at")
	if err != nil {
		return nil, err
	}
	snapshot.CreatedAt = created
	return &snapshot, nil
}
