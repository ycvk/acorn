package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func (s *Store) UpsertSessionSummary(ctx context.Context, summary runtimehistory.SessionSummary) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_summaries(session_id, source_run_id, run_status, summary, updated_at)
		 VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     source_run_id = excluded.source_run_id,
		     run_status = excluded.run_status,
		     summary = excluded.summary,
		     updated_at = excluded.updated_at`,
		strings.TrimSpace(summary.SessionID),
		strings.TrimSpace(summary.SourceRunID),
		strings.TrimSpace(summary.RunStatus),
		strings.TrimSpace(summary.Summary),
		formatTimestamp(summary.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert session summary: %w", err)
	}
	return nil
}

func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*runtimehistory.SessionSummary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, source_run_id, run_status, summary, updated_at
		 FROM session_summaries WHERE session_id = ?`,
		strings.TrimSpace(sessionID),
	)
	var (
		record    runtimehistory.SessionSummary
		updatedAt string
	)
	if err := row.Scan(&record.SessionID, &record.SourceRunID, &record.RunStatus, &record.Summary, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session summary: %w", err)
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, updatedAt, "session_summary.updated_at")
	if err != nil {
		return nil, err
	}
	record.UpdatedAt = parsed
	return &record, nil
}

func (s *Store) ListAllSessionSummaries(ctx context.Context) ([]runtimehistory.SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, source_run_id, run_status, summary, updated_at
		 FROM session_summaries
		 ORDER BY session_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all session summaries: %w", err)
	}
	defer rows.Close()

	items := make([]runtimehistory.SessionSummary, 0)
	for rows.Next() {
		var (
			record    runtimehistory.SessionSummary
			updatedAt string
		)
		if err := rows.Scan(&record.SessionID, &record.SourceRunID, &record.RunStatus, &record.Summary, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan session summary: %w", err)
		}
		parsed, err := parseTimestamp(fixedTimestampLayout, updatedAt, "session_summary.updated_at")
		if err != nil {
			return nil, err
		}
		record.UpdatedAt = parsed
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session summaries: %w", err)
	}
	return items, nil
}
