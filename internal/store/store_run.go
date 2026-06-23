package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/domain"
)

func (s *Store) CreateRun(ctx context.Context, runID, input string) error {
	return s.CreateRunWithParams(ctx, RunCreateParams{
		RunID: runID,
		Input: input,
	})
}

func (s *Store) CreateRunWithSession(ctx context.Context, runID, sessionID string, turnIndex int, input string) error {
	return s.CreateRunWithParams(ctx, RunCreateParams{
		RunID:     runID,
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
	})
}

func (s *Store) CreateRunWithParams(ctx context.Context, params RunCreateParams) error {
	now := formatTimestamp(time.Now())
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs(run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, updated_at) VALUES(?, ?, ?, ?, ?, '', '', ?, ?)`,
		params.RunID,
		params.SessionID,
		params.TurnIndex,
		string(domain.RunStatusRunning),
		params.Input,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (s *Store) CreateBoundRun(ctx context.Context, runID, sessionID string, turnIndex int, input string) error {
	return s.CreateBoundRunWithParams(ctx, RunCreateParams{
		RunID:     runID,
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
	})
}

func (s *Store) CreateBoundRunWithParams(ctx context.Context, params RunCreateParams) error {
	if err := s.CreateRunWithParams(ctx, params); err != nil {
		return err
	}
	if params.SessionID == "" {
		return nil
	}
	var bindErr error
	if params.BoundMessageID > 0 {
		bindErr = s.BindUserMessageRunIDByID(ctx, params.BoundMessageID, params.RunID)
	} else {
		bindErr = s.BindLatestUserMessageRunID(ctx, params.SessionID, params.TurnIndex, params.RunID)
	}
	if bindErr != nil {
		if _, cleanupErr := s.db.Exec(`DELETE FROM runs WHERE run_id = ?`, params.RunID); cleanupErr != nil {
			return fmt.Errorf("bind user message run id: %w (cleanup failed: %v)", bindErr, cleanupErr)
		}
		return bindErr
	}
	return nil
}

func (s *Store) AppendEventContext(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.EventRecord{}, fmt.Errorf("marshal event payload: %w", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		runID,
		kind,
		string(body),
		formatTimestamp(now),
	)
	if err != nil {
		return domain.EventRecord{}, fmt.Errorf("append event: %w", err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return domain.EventRecord{}, fmt.Errorf("read event sequence: %w", err)
	}
	return domain.EventRecord{Sequence: seq, RunID: runID, Kind: kind, Payload: payload, CreatedAt: now}, nil
}

func (s *Store) FinishRunContext(ctx context.Context, runID string, status domain.RunStatus, output, errText string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, output_text = ?, error_text = ?, updated_at = ? WHERE run_id = ?`,
		string(status),
		output,
		errText,
		formatTimestamp(time.Now()),
		runID,
	)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

func (s *Store) UpdateRunOutputContext(ctx context.Context, runID, output string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET output_text = ?, updated_at = ? WHERE run_id = ?`,
		output,
		formatTimestamp(time.Now()),
		runID,
	)
	if err != nil {
		return fmt.Errorf("update run output: %w", err)
	}
	return nil
}

func (s *Store) MarkInterruptedContext(ctx context.Context, runID, output string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, output_text = ?, updated_at = ? WHERE run_id = ?`,
		string(domain.RunStatusInterrupted),
		output,
		formatTimestamp(time.Now()),
		runID,
	)
	if err != nil {
		return fmt.Errorf("mark interrupted: %w", err)
	}
	return nil
}

func (s *Store) LoadRun(ctx context.Context, runID string) (*domain.RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, updated_at FROM runs WHERE run_id = ?`, runID)
	rec, err := scanRunRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
		}
		return nil, fmt.Errorf("load run: %w", err)
	}
	return rec, nil
}

func (s *Store) ListActiveRuns(ctx context.Context, limit int) ([]domain.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, updated_at
		 FROM runs
		 WHERE status = ? AND session_id <> ''
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		string(domain.RunStatusRunning),
		normalizeRunListLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list active runs: %w", err)
	}
	return scanRunRows(rows, "list active runs")
}

func (s *Store) ListRecentTerminalRuns(ctx context.Context, limit int) ([]domain.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, updated_at
		 FROM runs
		 WHERE status IN (?, ?, ?) AND session_id <> ''
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		string(domain.RunStatusSucceeded),
		string(domain.RunStatusInterrupted),
		string(domain.RunStatusFailed),
		normalizeRunListLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list recent terminal runs: %w", err)
	}
	return scanRunRows(rows, "list recent terminal runs")
}

func normalizeRunListLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

func scanRunRows(rows *sql.Rows, source string) ([]domain.RunRecord, error) {
	defer rows.Close()
	items := make([]domain.RunRecord, 0)
	for rows.Next() {
		record, err := scanRunRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return items, nil
}

func (s *Store) LoadEvents(ctx context.Context, runID string) ([]domain.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? ORDER BY sequence ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	return scanEventRows(rows, runID)
}

func (s *Store) LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]domain.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? AND sequence > ? ORDER BY sequence ASC`, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("query events after: %w", err)
	}
	return scanEventRows(rows, runID)
}

func scanEventRows(rows *sql.Rows, runID string) ([]domain.EventRecord, error) {
	defer rows.Close()
	items := make([]domain.EventRecord, 0)
	for rows.Next() {
		var (
			seq     int64
			kind    string
			payload string
			created string
			body    any
		)
		if err := rows.Scan(&seq, &kind, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &body); err != nil {
			return nil, fmt.Errorf("unmarshal event payload run_id=%s sequence=%d kind=%s: %w", runID, seq, kind, err)
		}
		parsed, err := parseTimestamp(time.RFC3339Nano, created, "event.created_at")
		if err != nil {
			return nil, err
		}
		items = append(items, domain.EventRecord{Sequence: seq, RunID: runID, Kind: kind, Payload: body, CreatedAt: parsed})
	}
	return items, rows.Err()
}
