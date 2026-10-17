package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// CreateRun implements core.RunRepo. It creates a run record with status
// "running", executed by this store's process, and an empty finished_at
// (filled on completion). Binding of user messages is handled separately via
// BindLatestUserMessageRunID / BindUserMessageRunIDByID.
func (s *Store) CreateRun(ctx context.Context, params core.RunCreateParams) error {
	now := formatTimestamp(time.Now())
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO runs(run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at) VALUES(?, ?, ?, ?, ?, '', '', ?, '')`,
			params.RunID,
			params.SessionID,
			params.TurnIndex,
			string(core.RunStatusRunning),
			params.Input,
			now,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO run_processes(run_id, process) VALUES(?, ?)`, params.RunID, s.process)
		return err
	})
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

// AppendEvent implements core.EventRepo.
func (s *Store) AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return core.EventRecord{}, fmt.Errorf("marshal event payload: %w", err)
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
		return core.EventRecord{}, fmt.Errorf("append event: %w", err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return core.EventRecord{}, fmt.Errorf("read event sequence: %w", err)
	}
	return core.EventRecord{Sequence: seq, RunID: runID, Kind: kind, Payload: payload, CreatedAt: now}, nil
}

// FinishRun implements core.RunRepo.
func (s *Store) FinishRun(ctx context.Context, runID string, status core.RunStatus, output, errText string) error {
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE runs SET status = ?, output_text = ?, error_text = ?, finished_at = ? WHERE run_id = ?`,
			string(status),
			output,
			errText,
			formatTimestamp(time.Now()),
			runID,
		); err != nil {
			return err
		}
		return stopRunProcess(ctx, tx, runID)
	})
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

// stopRunProcess drops the process of a run that is no longer running.
func stopRunProcess(ctx context.Context, tx *sql.Tx, runID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM run_processes WHERE run_id = ?`, runID)
	return err
}

// UpdateRunOutput implements core.RunRepo.
func (s *Store) UpdateRunOutput(ctx context.Context, runID, output string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET output_text = ? WHERE run_id = ?`,
		output,
		runID,
	)
	if err != nil {
		return fmt.Errorf("update run output: %w", err)
	}
	return nil
}

// MarkInterrupted implements core.RunRepo.
func (s *Store) MarkInterrupted(ctx context.Context, runID, output string) error {
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE runs SET status = ?, output_text = ?, finished_at = ? WHERE run_id = ?`,
			string(core.RunStatusInterrupted),
			output,
			formatTimestamp(time.Now()),
			runID,
		); err != nil {
			return err
		}
		return stopRunProcess(ctx, tx, runID)
	})
	if err != nil {
		return fmt.Errorf("mark interrupted: %w", err)
	}
	return nil
}

// ResumeInterruptedRun moves an interrupted run back to running, executed by
// this store's process. The conditional update is the claim that lets exactly
// one resumer continue it.
func (s *Store) ResumeInterruptedRun(ctx context.Context, runID string) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`UPDATE runs SET status = ?, finished_at = '' WHERE run_id = ? AND status = ?`,
			string(core.RunStatusRunning),
			runID,
			string(core.RunStatusInterrupted),
		)
		if err != nil {
			return fmt.Errorf("resume interrupted run: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("resume interrupted run rows affected: %w", err)
		}
		if affected != 1 {
			return fmt.Errorf("%w: %s", core.ErrRunNotInterrupted, runID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_processes(run_id, process) VALUES(?, ?) ON CONFLICT(run_id) DO UPDATE SET process = excluded.process`, runID, s.process); err != nil {
			return fmt.Errorf("resume interrupted run: %w", err)
		}
		return nil
	})
}

// ListInterruptedRuns returns every interrupted run, oldest first.
func (s *Store) ListInterruptedRuns(ctx context.Context) ([]core.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at
		 FROM runs
		 WHERE status = ?
		 ORDER BY created_at ASC`,
		string(core.RunStatusInterrupted),
	)
	if err != nil {
		return nil, fmt.Errorf("list interrupted runs: %w", err)
	}
	return scanRunRows(rows, "list interrupted runs")
}

func (s *Store) LoadRun(ctx context.Context, runID string) (*core.RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at FROM runs WHERE run_id = ?`, runID)
	rec, err := scanRunRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", core.ErrRunNotFound, runID)
		}
		return nil, fmt.Errorf("load run: %w", err)
	}
	return rec, nil
}

// ListActiveRuns and ListRecentTerminalRuns list runs that belong to a thread.
func (s *Store) ListActiveRuns(ctx context.Context, limit int) ([]core.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.run_id, r.session_id, r.turn_index, r.status, r.input_text, r.output_text, r.error_text, r.created_at, r.finished_at
		 FROM runs r JOIN sessions s ON s.session_id = r.session_id
		 WHERE r.status = ?
		 ORDER BY r.created_at DESC
		 LIMIT ?`,
		string(core.RunStatusRunning),
		normalizeRunListLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list active runs: %w", err)
	}
	return scanRunRows(rows, "list active runs")
}

func (s *Store) ListRecentTerminalRuns(ctx context.Context, limit int) ([]core.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.run_id, r.session_id, r.turn_index, r.status, r.input_text, r.output_text, r.error_text, r.created_at, r.finished_at
		 FROM runs r JOIN sessions s ON s.session_id = r.session_id
		 WHERE r.status IN (?, ?, ?)
		 ORDER BY r.finished_at DESC
		 LIMIT ?`,
		string(core.RunStatusSucceeded),
		string(core.RunStatusInterrupted),
		string(core.RunStatusFailed),
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

func scanRunRows(rows *sql.Rows, source string) ([]core.RunRecord, error) {
	defer rows.Close()
	items := make([]core.RunRecord, 0)
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

func (s *Store) LoadEvents(ctx context.Context, runID string) ([]core.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? ORDER BY sequence ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	return scanEventRows(rows, runID)
}

func (s *Store) LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]core.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? AND sequence > ? ORDER BY sequence ASC`, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("query events after: %w", err)
	}
	return scanEventRows(rows, runID)
}

func scanEventRows(rows *sql.Rows, runID string) ([]core.EventRecord, error) {
	defer rows.Close()
	items := make([]core.EventRecord, 0)
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
		items = append(items, core.EventRecord{Sequence: seq, RunID: runID, Kind: kind, Payload: body, CreatedAt: parsed})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	return items, nil
}

// LoadToolCallEvents returns the tool.call.* events of the given runs keyed by
// run id, each in sequence order.
func (s *Store) LoadToolCallEvents(ctx context.Context, runIDs []string) (map[string][]core.EventRecord, error) {
	placeholders, args := inClause(runIDs)
	result := map[string][]core.EventRecord{}
	if len(args) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, sequence, kind, payload_json, created_at FROM events
		WHERE run_id IN (`+placeholders+`) AND kind IN ('tool.call.started', 'tool.call.succeeded', 'tool.call.failed')
		ORDER BY run_id, sequence`, args...)
	if err != nil {
		return nil, fmt.Errorf("load tool call events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			record           core.EventRecord
			payload, created string
		)
		if err := rows.Scan(&record.RunID, &record.Sequence, &record.Kind, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan tool call event: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &record.Payload); err != nil {
			return nil, fmt.Errorf("unmarshal event payload run_id=%s sequence=%d kind=%s: %w", record.RunID, record.Sequence, record.Kind, err)
		}
		if record.CreatedAt, err = parseTimestamp(time.RFC3339Nano, created, "event.created_at"); err != nil {
			return nil, err
		}
		result[record.RunID] = append(result[record.RunID], record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load tool call events: %w", err)
	}
	return result, nil
}
