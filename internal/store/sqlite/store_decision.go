package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/decision"
)

func (s *Store) SaveRunDecision(ctx context.Context, record decision.Record) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO run_decisions(run_id, session_id, action, intent, selected_skill_id, decision_reason, decision_profile_hash, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
		     session_id = excluded.session_id,
		     action = excluded.action,
		     intent = excluded.intent,
		     selected_skill_id = excluded.selected_skill_id,
		     decision_reason = excluded.decision_reason,
		     decision_profile_hash = excluded.decision_profile_hash,
		     created_at = excluded.created_at`,
		strings.TrimSpace(record.RunID),
		strings.TrimSpace(record.SessionID),
		string(record.Action),
		strings.TrimSpace(record.Intent),
		strings.TrimSpace(record.SelectedSkillID),
		strings.TrimSpace(record.DecisionReason),
		strings.TrimSpace(record.DecisionProfileHash),
		formatTimestamp(record.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save run decision: %w", err)
	}
	return nil
}

func (s *Store) LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT run_id, session_id, action, intent, selected_skill_id, decision_reason, decision_profile_hash, created_at
		 FROM run_decisions WHERE run_id = ?`,
		strings.TrimSpace(runID),
	)
	var (
		record    decision.Record
		action    string
		createdAt string
	)
	if err := row.Scan(
		&record.RunID,
		&record.SessionID,
		&action,
		&record.Intent,
		&record.SelectedSkillID,
		&record.DecisionReason,
		&record.DecisionProfileHash,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load run decision: %w", err)
	}
	record.Action = decision.Action(strings.TrimSpace(action))
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "run_decision.created_at")
	if err != nil {
		return nil, err
	}
	record.CreatedAt = parsed
	return &record, nil
}
