package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"database/sql"

	"github.com/ycvk/acorn/internal/domain"
)

func (s *Store) HasAssistantMessageForRunContent(runID, content string) (bool, error) {
	row := s.db.QueryRow(`SELECT COUNT(1) FROM session_messages WHERE run_id = ? AND role = 'assistant' AND content = ?`, runID, content)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("assistant message for run: %w", err)
	}
	return count > 0, nil
}

func (s *Store) SyncAssistantMessageForRun(ctx context.Context, runID string) error {
	return s.syncAssistantMessageForRun(ctx, runID, "")
}

func (s *Store) SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status domain.RunStatus) error {
	return s.syncAssistantMessageForRun(ctx, runID, status)
}

// syncAssistantMessageForRun persists the assistant turn message for a run.
// The result-summary projection (tool results, plan evidence) was retired
// with the architecture refactor; the assistant message is now derived purely
// from the run record.
func (s *Store) syncAssistantMessageForRun(ctx context.Context, runID string, statusOverride domain.RunStatus) error {
	run, err := s.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	if statusOverride != "" {
		run.Status = statusOverride
	}
	if strings.TrimSpace(run.SessionID) == "" {
		return nil
	}
	content := run.Output
	if strings.TrimSpace(content) == "" {
		return nil
	}
	exists, err := s.HasAssistantMessageForRunContent(runID, content)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.AppendSessionMessage(ctx, run.SessionID, run.TurnIndex, "assistant", content, runID)
	return err
}

func (s *Store) LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*domain.RunRecord, error) {
	if len(sessionIDs) == 0 {
		return map[string]*domain.RunRecord{}, nil
	}

	seen := make(map[string]struct{}, len(sessionIDs))
	args := make([]any, 0, len(sessionIDs))
	placeholders := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		trimmed := strings.TrimSpace(sessionID)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		args = append(args, trimmed)
		placeholders = append(placeholders, "?")
	}
	if len(placeholders) == 0 {
		return map[string]*domain.RunRecord{}, nil
	}

	query := fmt.Sprintf(
		`WITH ranked_runs AS (
			SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at,
				ROW_NUMBER() OVER (
					PARTITION BY session_id
					ORDER BY turn_index DESC, finished_at DESC
				) AS row_num
			FROM runs
			WHERE session_id IN (%s)
		)
		SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at
		FROM ranked_runs
		WHERE row_num = 1`,
		strings.Join(placeholders, ", "),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load latest runs for sessions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*domain.RunRecord, len(placeholders))
	for rows.Next() {
		rec, err := scanRunRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("load latest runs for sessions: %w", err)
		}
		result[rec.SessionID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load latest runs for sessions: %w", err)
	}
	return result, nil
}

func (s *Store) LoadLatestRunForSession(ctx context.Context, sessionID string) (*domain.RunRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at
         FROM runs
         WHERE session_id = ?
         ORDER BY turn_index DESC, finished_at DESC
         LIMIT 1`,
		sessionID,
	)
	rec, err := scanRunRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load latest run for session: %w", err)
	}
	return rec, nil
}
