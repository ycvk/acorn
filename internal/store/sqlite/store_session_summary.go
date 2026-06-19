package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/sessionview"
	"github.com/ycvk/acorn/internal/store"
)

func (s *Store) UpsertSessionSummary(ctx context.Context, summary model.SessionSummary) error {
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

func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*model.SessionSummary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, source_run_id, run_status, summary, updated_at
		 FROM session_summaries WHERE session_id = ?`,
		strings.TrimSpace(sessionID),
	)
	var (
		record    model.SessionSummary
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

func (s *Store) ListAllSessionSummaries(ctx context.Context) ([]model.SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, source_run_id, run_status, summary, updated_at
		 FROM session_summaries
		 ORDER BY session_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all session summaries: %w", err)
	}
	defer rows.Close()

	items := make([]model.SessionSummary, 0)
	for rows.Next() {
		var (
			record    model.SessionSummary
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

// buildSessionMessageResultSummary reads the three runtime tables for a run and
// delegates the pure UI projection to internal/sessionview.
func (s *Store) buildSessionMessageResultSummary(ctx context.Context, runID string) (sessionview.ResultSummary, error) {
	records, err := s.LoadEvents(ctx, runID)
	if err != nil {
		return sessionview.ResultSummary{}, fmt.Errorf("build session result summary: load events: %w", err)
	}
	toolResults, err := s.ListByRun(ctx, runID)
	if err != nil {
		return sessionview.ResultSummary{}, fmt.Errorf("build session result summary: list tool results: %w", err)
	}
	plan, err := s.LoadPlanByRun(ctx, runID)
	if err != nil && !errors.Is(err, store.ErrPlanNotFound) {
		return sessionview.ResultSummary{}, fmt.Errorf("build session result summary: load plan: %w", err)
	}
	return sessionview.BuildResultSummary(records, toolResults, plan)
}

func (s *Store) HasAssistantMessageForRunContent(runID, content string) (bool, error) {
	row := s.db.QueryRow(`SELECT COUNT(1) FROM session_messages WHERE run_id = ? AND role = 'assistant' AND content = ?`, runID, content)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("assistant message for run: %w", err)
	}
	return count > 0, nil
}

func (s *Store) SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error {
	action, err := s.LoadPendingAction(ctx, actionID)
	if err != nil {
		return err
	}
	run, err := s.LoadRun(ctx, action.RunID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.SessionID) == "" {
		return nil
	}
	content, parts, err := sessionview.DecisionMessageForPendingAction(action)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}

	messageID, found, err := s.findDecisionSessionMessageID(ctx, action.RunID, action.ActionID)
	if err != nil {
		return err
	}
	if found {
		return s.UpdateSessionMessageWithParts(ctx, messageID, content, parts)
	}
	_, err = s.AppendSessionMessageWithParts(run.SessionID, run.TurnIndex, "assistant", content, parts, action.RunID)
	return err
}

func (s *Store) findDecisionSessionMessageID(ctx context.Context, runID, actionID string) (int64, bool, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, content_parts
		 FROM session_messages
		 WHERE run_id = ? AND role = 'assistant'
		 ORDER BY id ASC`,
		runID,
	)
	if err != nil {
		return 0, false, fmt.Errorf("find decision session message: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id           int64
			contentParts string
		)
		if err := rows.Scan(&id, &contentParts); err != nil {
			return 0, false, fmt.Errorf("find decision session message scan: %w", err)
		}
		matches, err := sessionview.DecisionMessageHasActionID(contentParts, actionID)
		if err != nil {
			return 0, false, fmt.Errorf("find decision session message %d content parts: %w", id, err)
		}
		if matches {
			return id, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("find decision session message rows: %w", err)
	}
	return 0, false, nil
}

func (s *Store) SyncAssistantMessageForRun(ctx context.Context, runID string) error {
	return s.syncAssistantMessageForRun(ctx, runID, "")
}

func (s *Store) SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status events.RunStatus) error {
	return s.syncAssistantMessageForRun(ctx, runID, status)
}

func (s *Store) syncAssistantMessageForRun(ctx context.Context, runID string, statusOverride events.RunStatus) error {
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
	summary := sessionview.ResultSummary{}
	if run.Status == events.RunStatusSucceeded {
		summary, err = s.buildSessionMessageResultSummary(ctx, run.RunID)
		if err != nil {
			return err
		}
	}
	content, parts, err := sessionview.AssistantMessageForRun(run, summary)
	if err != nil {
		return err
	}
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
	_, err = s.AppendSessionMessageWithParts(run.SessionID, run.TurnIndex, "assistant", content, parts, runID)
	return err
}

func (s *Store) LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*events.RunRecord, error) {
	if len(sessionIDs) == 0 {
		return map[string]*events.RunRecord{}, nil
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
		return map[string]*events.RunRecord{}, nil
	}

	query := fmt.Sprintf(
		`WITH ranked_runs AS (
			SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY session_id
					ORDER BY turn_index DESC, updated_at DESC
				) AS row_num
			FROM runs
			WHERE session_id IN (%s)
		)
		SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at
		FROM ranked_runs
		WHERE row_num = 1`,
		strings.Join(placeholders, ", "),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load latest runs for sessions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*events.RunRecord, len(placeholders))
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

func (s *Store) LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at
         FROM runs
         WHERE session_id = ?
         ORDER BY turn_index DESC, updated_at DESC
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

func (s *Store) GetConversationHistorySegment(ctx context.Context, segmentID int64) (*model.HistoryHit, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, run_status, user_content || char(10) || assistant_content, created_at
		 FROM conversation_segments WHERE id = ?`,
		segmentID,
	)
	hit, err := scanHistoryHit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation history segment: %w", err)
	}
	return hit, nil
}

func (s *Store) GetConversationHistorySegmentByRunID(ctx context.Context, runID string) (*model.HistoryHit, error) {
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID == "" {
		return nil, errors.New("run id is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, run_status, user_content || char(10) || assistant_content, created_at
		 FROM conversation_segments WHERE run_id = ?`,
		trimmedRunID,
	)
	hit, err := scanHistoryHit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation history segment by run: %w", err)
	}
	return hit, nil
}

func scanHistoryHit(scanner interface{ Scan(dest ...any) error }) (*model.HistoryHit, error) {
	var (
		hit       model.HistoryHit
		createdAt string
	)
	if err := scanner.Scan(&hit.SegmentID, &hit.SessionID, &hit.RunID, &hit.RunStatus, &hit.Content, &createdAt); err != nil {
		return nil, err
	}
	timestamp, err := parseTimestamp(fixedTimestampLayout, createdAt, "conversation_segment.created_at")
	if err != nil {
		return nil, err
	}
	hit.Timestamp = timestamp
	return &hit, nil
}
