package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// followUpsTable is shared with the migration that adds it. path_size is
// NULL while the watched path was missing.
const followUpsTable = `CREATE TABLE IF NOT EXISTS follow_ups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL DEFAULT '',
    task TEXT NOT NULL,
    job_id TEXT NOT NULL DEFAULT '',
    output_contains TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    deadline_at TEXT NOT NULL,
    state TEXT NOT NULL,
    scanned_offset INTEGER NOT NULL DEFAULT 0,
    path_size INTEGER,
    reason TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_follow_ups_state ON follow_ups(state, deadline_at);`

// addFollowUpsAndGoals (31 to 32) adds the follow-ups that wake the agent on
// workstation events and the goals it continues toward.
func addFollowUpsAndGoals(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{followUpsTable, goalsTable} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// followUpClaimTimeout is how long a claimed follow-up may go without a run
// before it waits again.
const followUpClaimTimeout = 5 * time.Minute

const followUpColumns = `id,session_id,source_run_id,task,job_id,output_contains,path,deadline_at,state,scanned_offset,path_size,reason,run_id,created_at,updated_at`

// AddFollowUp implements core.FollowUpStore.
func (s *Store) AddFollowUp(ctx context.Context, f core.FollowUp) (core.FollowUp, error) {
	switch {
	case strings.TrimSpace(f.SessionID) == "" || strings.TrimSpace(f.Task) == "":
		return core.FollowUp{}, errors.New("follow-up requires session_id and task")
	case f.CreatedAt.IsZero() || !f.DeadlineAt.After(f.CreatedAt):
		return core.FollowUp{}, errors.New("follow-up requires created_at and a later deadline_at")
	case (f.JobID == "") == (f.Path == ""):
		return core.FollowUp{}, errors.New("follow-up requires either job_id or path")
	case f.OutputContains != "" && f.JobID == "":
		return core.FollowUp{}, errors.New("follow-up output_contains requires job_id")
	}
	f.State, f.ScannedOffset, f.PathSize, f.Reason, f.RunID, f.UpdatedAt = core.FollowUpWaiting, 0, nil, "", "", f.CreatedAt
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id = ?`, f.SessionID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: %s", core.ErrSessionNotFound, f.SessionID)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO follow_ups(session_id,source_run_id,task,job_id,output_contains,path,deadline_at,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			f.SessionID, f.SourceRunID, f.Task, f.JobID, f.OutputContains, f.Path, formatTimestamp(f.DeadlineAt), f.State, formatTimestamp(f.CreatedAt), formatTimestamp(f.UpdatedAt))
		if err != nil {
			return err
		}
		f.ID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return core.FollowUp{}, fmt.Errorf("add follow-up: %w", err)
	}
	return f, nil
}

// WaitingFollowUps implements core.FollowUpStore.
func (s *Store) WaitingFollowUps(ctx context.Context, sessionID string) ([]core.FollowUp, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+followUpColumns+` FROM follow_ups WHERE state = ? AND (? = '' OR session_id = ?) ORDER BY deadline_at, id`, core.FollowUpWaiting, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list follow-ups: %w", err)
	}
	out := []core.FollowUp{}
	err = scanRows(rows, func(scan func(...any) error) error {
		f, err := scanFollowUp(scannerFunc(scan))
		if err == nil {
			out = append(out, f)
		}
		return err
	})
	return out, err
}

func scanFollowUp(row interface{ Scan(...any) error }) (core.FollowUp, error) {
	var f core.FollowUp
	var deadline, created, updated string
	var size sql.NullInt64
	if err := row.Scan(&f.ID, &f.SessionID, &f.SourceRunID, &f.Task, &f.JobID, &f.OutputContains, &f.Path, &deadline, &f.State, &f.ScannedOffset, &size, &f.Reason, &f.RunID, &created, &updated); err != nil {
		return f, err
	}
	if size.Valid {
		f.PathSize = &size.Int64
	}
	var err error
	if f.DeadlineAt, err = parseTimestamp(deadline, "follow_ups.deadline_at"); err != nil {
		return f, err
	}
	if f.CreatedAt, err = parseTimestamp(created, "follow_ups.created_at"); err != nil {
		return f, err
	}
	f.UpdatedAt, err = parseTimestamp(updated, "follow_ups.updated_at")
	return f, err
}

// RecordFollowUpCheck implements core.FollowUpStore.
func (s *Store) RecordFollowUpCheck(ctx context.Context, id, scannedOffset int64, pathSize *int64, now time.Time) error {
	var size any
	if pathSize != nil {
		size = *pathSize
	}
	return s.changeFollowUp(ctx, id, `UPDATE follow_ups SET scanned_offset = ?, path_size = ?, updated_at = ? WHERE id = ? AND state = ?`,
		scannedOffset, size, formatTimestamp(now), id, core.FollowUpWaiting)
}

// ClaimFollowUp implements core.FollowUpStore.
func (s *Store) ClaimFollowUp(ctx context.Context, id int64, reason string, now time.Time) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("claim follow-up requires a reason")
	}
	return s.changeFollowUp(ctx, id, `UPDATE follow_ups SET state = ?, reason = ?, updated_at = ? WHERE id = ? AND state = ?`,
		core.FollowUpClaimed, reason, formatTimestamp(now), id, core.FollowUpWaiting)
}

// StartFollowUp implements core.FollowUpStore.
func (s *Store) StartFollowUp(ctx context.Context, id int64, runID string, now time.Time) error {
	if runID == "" {
		return errors.New("start follow-up requires a run_id")
	}
	return s.changeFollowUp(ctx, id, `UPDATE follow_ups SET state = ?, run_id = ?, updated_at = ? WHERE id = ? AND state = ?`,
		core.FollowUpFired, runID, formatTimestamp(now), id, core.FollowUpClaimed)
}

// ReleaseFollowUp implements core.FollowUpStore.
func (s *Store) ReleaseFollowUp(ctx context.Context, id int64, now time.Time) error {
	return s.changeFollowUp(ctx, id, `UPDATE follow_ups SET state = ?, reason = '', updated_at = ? WHERE id = ? AND state = ?`,
		core.FollowUpWaiting, formatTimestamp(now), id, core.FollowUpClaimed)
}

// RecoverFollowUpClaims implements core.FollowUpStore.
func (s *Store) RecoverFollowUpClaims(ctx context.Context, now time.Time) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE follow_ups SET state = ?, reason = '', updated_at = ? WHERE state = ? AND updated_at <= ?`,
			core.FollowUpWaiting, formatTimestamp(now), core.FollowUpClaimed, formatTimestamp(now.Add(-followUpClaimTimeout)))
		return err
	})
}

// CancelFollowUp implements core.FollowUpStore.
func (s *Store) CancelFollowUp(ctx context.Context, sessionID string, id int64, now time.Time) error {
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var state string
		err := tx.QueryRowContext(ctx, `SELECT state FROM follow_ups WHERE id = ? AND (? = '' OR session_id = ?)`, id, sessionID, sessionID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: #%d", core.ErrFollowUpNotFound, id)
		}
		if err != nil {
			return err
		}
		if state != core.FollowUpWaiting {
			return fmt.Errorf("%w: #%d is %s", core.ErrFollowUpNotWaiting, id, state)
		}
		_, err = tx.ExecContext(ctx, `UPDATE follow_ups SET state = ?, updated_at = ? WHERE id = ?`, core.FollowUpCancelled, formatTimestamp(now), id)
		return err
	})
	if err != nil {
		return fmt.Errorf("cancel follow-up: %w", err)
	}
	return nil
}

// changeFollowUp runs a conditional update of follow-up id; when no row
// changes the follow-up is missing or no longer in the state it required.
func (s *Store) changeFollowUp(ctx context.Context, id int64, query string, args ...any) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
		var state string
		err = tx.QueryRowContext(ctx, `SELECT state FROM follow_ups WHERE id = ?`, id).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: #%d", core.ErrFollowUpNotFound, id)
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: #%d is %s", core.ErrFollowUpNotWaiting, id, state)
	})
}
