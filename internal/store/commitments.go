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

const commitmentColumns = `id,content,state,session_id,source_run_id,concern_id,wake_at,recurrence,created_at,updated_at`

func (s *Store) AddCommitment(ctx context.Context, c core.Commitment) (core.Commitment, error) {
	if strings.TrimSpace(c.Content) == "" || c.CreatedAt.IsZero() || c.WakeAt.IsZero() {
		return core.Commitment{}, errors.New("commitment requires content, created_at and wake_at")
	}
	if c.State != "" && c.State != "scheduled" {
		return core.Commitment{}, errors.New("new commitment state must be scheduled")
	}
	c.State = "scheduled"
	c.UpdatedAt = c.CreatedAt
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if c.ConcernID != "" {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_concerns c WHERE id=? AND state IN ('active','waiting') AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=c.source_id)`, c.ConcernID).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return core.ErrMemoryNotFound
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO commitments(content,state,session_id,source_run_id,concern_id,wake_at,recurrence,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, c.Content, c.State, c.SessionID, c.SourceRunID, c.ConcernID, formatTimestamp(c.WakeAt), c.Recurrence, formatTimestamp(c.CreatedAt), formatTimestamp(c.UpdatedAt))
		if err != nil {
			return err
		}
		c.ID, err = result.LastInsertId()
		return err
	})
	return c, err
}

func scanCommitment(row interface{ Scan(...any) error }) (core.Commitment, error) {
	var c core.Commitment
	var wake, created, updated string
	if err := row.Scan(&c.ID, &c.Content, &c.State, &c.SessionID, &c.SourceRunID, &c.ConcernID, &wake, &c.Recurrence, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, core.ErrCommitmentNotFound
		}
		return c, err
	}
	var err error
	if c.WakeAt, err = parseOptionalTime(wake, "commitments.wake_at"); err != nil {
		return c, err
	}
	if c.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return c, err
	}
	c.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return c, err
}

func (s *Store) LoadCommitment(ctx context.Context, id int64) (core.Commitment, error) {
	return scanCommitment(s.db.QueryRowContext(ctx, `SELECT `+commitmentColumns+` FROM commitments WHERE id=?`, id))
}

func (s *Store) ListCommitments(ctx context.Context, activeOnly bool) ([]core.Commitment, error) {
	return s.queryCommitments(ctx, `SELECT `+commitmentColumns+` FROM commitments WHERE ?=0 OR state IN ('scheduled','due') ORDER BY wake_at,id`, activeOnly)
}

func (s *Store) DueCommitments(ctx context.Context, now time.Time) ([]core.Commitment, error) {
	return s.queryCommitments(ctx, `SELECT `+commitmentColumns+` FROM commitments WHERE state='scheduled' AND wake_at<=? ORDER BY wake_at,id`, formatTimestamp(now))
}

func (s *Store) queryCommitments(ctx context.Context, query string, args ...any) ([]core.Commitment, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []core.Commitment{}
	err = scanRows(rows, func(scan func(...any) error) error {
		c, err := scanCommitment(scannerFunc(scan))
		if err == nil {
			out = append(out, c)
		}
		return err
	})
	return out, err
}

func (s *Store) ClaimCommitment(ctx context.Context, id int64, now time.Time) (core.CommitmentOccurrence, error) {
	var out core.CommitmentOccurrence
	if now.IsZero() {
		return out, errors.New("commitment claim requires now")
	}
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		c, err := scanCommitment(tx.QueryRowContext(ctx, `SELECT `+commitmentColumns+` FROM commitments WHERE id=?`, id))
		if err != nil {
			return err
		}
		if c.State != "scheduled" || c.WakeAt.After(now) {
			return core.ErrCommitmentNotDue
		}
		result, err := tx.ExecContext(ctx, `UPDATE commitments SET state='due',updated_at=? WHERE id=? AND state='scheduled' AND wake_at<=?`, formatTimestamp(now), id, formatTimestamp(now))
		if err := commitmentChanged(result, err); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `INSERT INTO commitment_occurrences(commitment_id,due_at,state,updated_at) VALUES(?,?,'claimed',?)`, id, formatTimestamp(c.WakeAt), formatTimestamp(now))
		if err != nil {
			return err
		}
		occurrenceID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		out = core.CommitmentOccurrence{ID: occurrenceID, CommitmentID: id, DueAt: c.WakeAt, State: "claimed", UpdatedAt: now}
		return nil
	})
	return out, err
}

func commitmentChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return core.ErrCommitmentNotDue
	}
	return nil
}

func (s *Store) StartCommitmentOccurrence(ctx context.Context, occ core.CommitmentOccurrence, runID string, next, now time.Time) error {
	if runID == "" || now.IsZero() {
		return errors.New("start commitment requires run_id and now")
	}
	if !next.IsZero() && !next.After(now) {
		return errors.New("next commitment must be after now")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE commitment_occurrences SET state='due',run_id=?,updated_at=? WHERE id=? AND commitment_id=? AND state='claimed'`, runID, formatTimestamp(now), occ.ID, occ.CommitmentID)
		if err := commitmentChanged(result, err); err != nil {
			return err
		}
		if !next.IsZero() {
			result, err = tx.ExecContext(ctx, `UPDATE commitments SET state='scheduled',wake_at=?,updated_at=? WHERE id=? AND state='due'`, formatTimestamp(next), formatTimestamp(now), occ.CommitmentID)
			return commitmentChanged(result, err)
		}
		return nil
	})
}

func (s *Store) RetryCommitment(ctx context.Context, occ core.CommitmentOccurrence, next, now time.Time) error {
	if now.IsZero() || !next.After(now) {
		return errors.New("retry commitment requires future next and now")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE commitment_occurrences SET state='start_failed',updated_at=? WHERE id=? AND commitment_id=? AND state='claimed'`, formatTimestamp(now), occ.ID, occ.CommitmentID)
		if err := commitmentChanged(result, err); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `UPDATE commitments SET state='scheduled',wake_at=?,updated_at=? WHERE id=? AND state='due'`, formatTimestamp(next), formatTimestamp(now), occ.CommitmentID)
		return commitmentChanged(result, err)
	})
}

func (s *Store) ListDueOccurrences(ctx context.Context) ([]core.CommitmentOccurrence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,commitment_id,due_at,state,run_id,evidence_source_id,updated_at FROM commitment_occurrences WHERE state IN ('claimed','due') ORDER BY due_at,id`)
	if err != nil {
		return nil, err
	}
	out := []core.CommitmentOccurrence{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var o core.CommitmentOccurrence
		var due, updated string
		if err := scan(&o.ID, &o.CommitmentID, &due, &o.State, &o.RunID, &o.EvidenceSourceID, &updated); err != nil {
			return err
		}
		var err error
		if o.DueAt, err = parseOptionalTime(due, "commitment_occurrences.due_at"); err != nil {
			return err
		}
		if o.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return err
		}
		out = append(out, o)
		return nil
	})
	return out, err
}

func (s *Store) SettleCommitment(ctx context.Context, change core.CommitmentSettlement) error {
	if change.Now.IsZero() {
		return errors.New("settle commitment requires now")
	}
	if change.Action != "done" && change.Action != "cancel" {
		return errors.New("settle action must be done or cancel")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		c, err := scanCommitment(tx.QueryRowContext(ctx, `SELECT `+commitmentColumns+` FROM commitments WHERE id=?`, change.CommitmentID))
		if err != nil {
			return err
		}
		if c.State == "completed" || c.State == "cancelled" {
			return fmt.Errorf("commitment %d is %s", c.ID, c.State)
		}
		if change.Action == "cancel" {
			if _, err := tx.ExecContext(ctx, `UPDATE commitments SET state='cancelled',updated_at=? WHERE id=?`, formatTimestamp(change.Now), c.ID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE commitment_occurrences SET state='cancelled',updated_at=? WHERE commitment_id=? AND state IN ('claimed','due')`, formatTimestamp(change.Now), c.ID)
			return err
		}
		source, err := loadMemorySource(ctx, s.memoryConnection(tx), change.SourceID, true)
		if err != nil {
			return err
		}
		if source.Speaker != "owner" && source.Speaker != "tool" {
			return errors.New("commitment completion requires owner confirmation or tool execution evidence")
		}
		if source.Speaker == "tool" && (change.RunID == "" || source.RunID != change.RunID) {
			return errors.New("completion evidence must belong to this run")
		}
		result, err := tx.ExecContext(ctx, `UPDATE commitment_occurrences SET state='completed',evidence_source_id=?,updated_at=? WHERE id=? AND commitment_id=? AND state='due'`, source.ID, formatTimestamp(change.Now), change.OccurrenceID, c.ID)
		if err := commitmentChanged(result, err); err != nil {
			return err
		}
		if c.State == "due" {
			var remaining int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM commitment_occurrences WHERE commitment_id=? AND state IN ('claimed','due')`, c.ID).Scan(&remaining); err != nil {
				return err
			}
			if remaining == 0 {
				_, err = tx.ExecContext(ctx, `UPDATE commitments SET state='completed',updated_at=? WHERE id=?`, formatTimestamp(change.Now), c.ID)
				return err
			}
		}
		return nil
	})
}
