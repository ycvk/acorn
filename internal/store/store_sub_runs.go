package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

var _ core.SubRunStore = (*Store)(nil)

// subRunsTable is shared with the migration that adds it. A sub-run is a run
// of its parent's conversation; its status, output and error are its run's.
const subRunsTable = `CREATE TABLE IF NOT EXISTS sub_runs (
    run_id TEXT PRIMARY KEY,
    parent_run_id TEXT NOT NULL,
    task INTEGER NOT NULL,
    attempt INTEGER NOT NULL,
    brief TEXT NOT NULL,
    submission TEXT NOT NULL DEFAULT '',
    verdict TEXT NOT NULL DEFAULT '',
    feedback TEXT NOT NULL DEFAULT '',
    delivered INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    UNIQUE(parent_run_id, task, attempt)
);`

// notSubRun keeps a query on runs r to the conversation's own runs.
const notSubRun = `NOT EXISTS(SELECT 1 FROM sub_runs WHERE sub_runs.run_id = r.run_id)`

// addSubRuns (33 to 34) records the sub-runs runs delegate.
func addSubRuns(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, subRunsTable)
	return err
}

const subRunColumns = `s.run_id, s.parent_run_id, s.task, s.attempt, s.brief, s.submission, s.verdict, s.feedback, s.delivered, s.created_at, r.status, r.output_text, r.error_text, r.finished_at`

// StartSubRun implements core.SubRunStore.
func (s *Store) StartSubRun(ctx context.Context, start core.SubRunStart) (core.SubRun, error) {
	switch {
	case strings.TrimSpace(start.RunID) == "" || strings.TrimSpace(start.ParentRunID) == "":
		return core.SubRun{}, errors.New("sub-run requires run_id and parent_run_id")
	case strings.TrimSpace(start.Brief.Objective) == "" || strings.TrimSpace(start.Input) == "":
		return core.SubRun{}, errors.New("sub-run requires a brief objective and input")
	case start.Limit < 1 || start.CreatedAt.IsZero() || start.Task < 0:
		return core.SubRun{}, errors.New("sub-run requires a positive limit, created_at and a task that is not negative")
	case start.Task > 0 && strings.TrimSpace(start.Feedback) == "":
		return core.SubRun{}, errors.New("sub-run redo requires feedback")
	}
	brief, err := json.Marshal(start.Brief)
	if err != nil {
		return core.SubRun{}, fmt.Errorf("marshal sub-run brief: %w", err)
	}
	var out core.SubRun
	err = s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var sessionID, status string
		var turn int
		err := tx.QueryRowContext(ctx, `SELECT session_id, turn_index, status FROM runs WHERE run_id = ?`, start.ParentRunID).Scan(&sessionID, &turn, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", core.ErrRunNotFound, start.ParentRunID)
		}
		if err != nil {
			return err
		}
		if status != string(core.RunStatusRunning) {
			return fmt.Errorf("parent run %s is %s, not running", start.ParentRunID, status)
		}
		var nested, delegated int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sub_runs WHERE run_id = ?), (SELECT COUNT(*) FROM sub_runs WHERE parent_run_id = ?)`, start.ParentRunID, start.ParentRunID).Scan(&nested, &delegated); err != nil {
			return err
		}
		if nested == 1 {
			return fmt.Errorf("sub-run %s cannot delegate", start.ParentRunID)
		}
		if delegated >= start.Limit {
			return fmt.Errorf("%w: %d of %d", core.ErrSubRunLimit, delegated, start.Limit)
		}
		task, attempt := start.Task, 1
		if task == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(task), 0) + 1 FROM sub_runs WHERE parent_run_id = ?`, start.ParentRunID).Scan(&task); err != nil {
				return err
			}
		} else {
			latest, err := latestSubRun(ctx, tx, start.ParentRunID, task)
			if err != nil {
				return err
			}
			if err := reviewable(latest); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE sub_runs SET verdict = ?, feedback = ? WHERE run_id = ?`, core.SubRunRedo, start.Feedback, latest.RunID); err != nil {
				return err
			}
			attempt = latest.Attempt + 1
		}
		created := formatTimestamp(start.CreatedAt)
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`INSERT INTO runs(run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at) VALUES(?, ?, ?, ?, ?, '', '', ?, '')`,
				[]any{start.RunID, sessionID, turn, string(core.RunStatusRunning), start.Input, created}},
			{`INSERT INTO run_processes(run_id, process) VALUES(?, ?)`, []any{start.RunID, s.process}},
			{`INSERT INTO sub_runs(run_id, parent_run_id, task, attempt, brief, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
				[]any{start.RunID, start.ParentRunID, task, attempt, string(brief), created}},
		} {
			if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
				return err
			}
		}
		out = core.SubRun{
			RunID: start.RunID, ParentRunID: start.ParentRunID, Task: task, Attempt: attempt, Brief: start.Brief,
			Status: core.RunStatusRunning, CreatedAt: start.CreatedAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return core.SubRun{}, fmt.Errorf("start sub-run of %s: %w", start.ParentRunID, err)
	}
	return out, nil
}

// reviewable fails for an attempt that cannot get a verdict.
func reviewable(latest core.SubRun) error {
	if !latest.Reported() {
		return fmt.Errorf("%w: task %d attempt %d", core.ErrSubRunRunning, latest.Task, latest.Attempt)
	}
	if latest.Verdict != "" {
		return fmt.Errorf("%w: task %d attempt %d is %s", core.ErrSubRunReviewed, latest.Task, latest.Attempt, latest.Verdict)
	}
	return nil
}

// AcceptSubRun implements core.SubRunStore.
func (s *Store) AcceptSubRun(ctx context.Context, parentRunID string, task int) (core.SubRun, error) {
	var out core.SubRun
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		latest, err := latestSubRun(ctx, tx, parentRunID, task)
		if err != nil {
			return err
		}
		if err := reviewable(latest); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sub_runs SET verdict = ? WHERE run_id = ?`, core.SubRunAccepted, latest.RunID); err != nil {
			return err
		}
		latest.Verdict = core.SubRunAccepted
		out = latest
		return nil
	})
	if err != nil {
		return core.SubRun{}, fmt.Errorf("accept task %d of %s: %w", task, parentRunID, err)
	}
	return out, nil
}

// SubmitSubRun implements core.SubRunStore.
func (s *Store) SubmitSubRun(ctx context.Context, runID string, submission core.SubRunSubmission) error {
	if strings.TrimSpace(submission.Summary) == "" {
		return errors.New("sub-run submission requires a summary")
	}
	data, err := json.Marshal(submission)
	if err != nil {
		return fmt.Errorf("marshal sub-run submission: %w", err)
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT r.status FROM sub_runs s JOIN runs r ON r.run_id = s.run_id WHERE s.run_id = ?`, runID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", core.ErrSubRunNotFound, runID)
		}
		if err != nil {
			return fmt.Errorf("submit sub-run %s: %w", runID, err)
		}
		if status != string(core.RunStatusRunning) {
			return fmt.Errorf("submit sub-run %s: it is %s, not running", runID, status)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sub_runs SET submission = ? WHERE run_id = ?`, string(data), runID); err != nil {
			return fmt.Errorf("submit sub-run %s: %w", runID, err)
		}
		return nil
	})
}

// SubRuns implements core.SubRunStore.
func (s *Store) SubRuns(ctx context.Context, parentRunID string) ([]core.SubRun, error) {
	runs, err := querySubRuns(ctx, s.db, `WHERE s.parent_run_id = ? ORDER BY s.task, s.attempt`, parentRunID)
	if err != nil {
		return nil, fmt.Errorf("sub-runs of %s: %w", parentRunID, err)
	}
	return runs, nil
}

// LatestSubRun implements core.SubRunStore.
func (s *Store) LatestSubRun(ctx context.Context, parentRunID string, task int) (core.SubRun, error) {
	latest, err := latestSubRun(ctx, s.db, parentRunID, task)
	if err != nil {
		return core.SubRun{}, fmt.Errorf("task %d of %s: %w", task, parentRunID, err)
	}
	return latest, nil
}

// TakeSubRunReports implements core.SubRunStore.
func (s *Store) TakeSubRunReports(ctx context.Context, parentRunID string) ([]core.SubRun, error) {
	var taken []core.SubRun
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var err error
		taken, err = querySubRuns(ctx, tx, `WHERE s.parent_run_id = ? AND s.delivered = 0 AND r.status <> 'running' ORDER BY r.finished_at, s.task, s.attempt`, parentRunID)
		if err != nil {
			return err
		}
		for i := range taken {
			if _, err := tx.ExecContext(ctx, `UPDATE sub_runs SET delivered = 1 WHERE run_id = ?`, taken[i].RunID); err != nil {
				return err
			}
			taken[i].Delivered = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("take sub-run reports of %s: %w", parentRunID, err)
	}
	return taken, nil
}

func latestSubRun(ctx context.Context, q memorySQL, parentRunID string, task int) (core.SubRun, error) {
	runs, err := querySubRuns(ctx, q, `WHERE s.parent_run_id = ? AND s.task = ? ORDER BY s.attempt DESC LIMIT 1`, parentRunID, task)
	if err != nil {
		return core.SubRun{}, err
	}
	if len(runs) == 0 {
		return core.SubRun{}, fmt.Errorf("%w: task %d", core.ErrSubRunNotFound, task)
	}
	return runs[0], nil
}

func querySubRuns(ctx context.Context, q memorySQL, where string, args ...any) ([]core.SubRun, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+subRunColumns+` FROM sub_runs s JOIN runs r ON r.run_id = s.run_id `+where, args...)
	if err != nil {
		return nil, err
	}
	var out []core.SubRun
	err = scanRows(rows, func(scan func(...any) error) error {
		var sub core.SubRun
		var brief, submission, created, status, finished string
		if err := scan(&sub.RunID, &sub.ParentRunID, &sub.Task, &sub.Attempt, &brief, &submission, &sub.Verdict, &sub.Feedback, &sub.Delivered, &created, &status, &sub.Output, &sub.Error, &finished); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(brief), &sub.Brief); err != nil {
			return fmt.Errorf("sub-run %s brief: %w", sub.RunID, err)
		}
		if submission != "" {
			sub.Submission = &core.SubRunSubmission{}
			if err := json.Unmarshal([]byte(submission), sub.Submission); err != nil {
				return fmt.Errorf("sub-run %s submission: %w", sub.RunID, err)
			}
		}
		sub.Status = core.RunStatus(status)
		var err error
		if sub.CreatedAt, err = parseTimestamp(created, "sub-run created_at"); err != nil {
			return err
		}
		if finished != "" {
			if sub.FinishedAt, err = parseTimestamp(finished, "sub-run finished_at"); err != nil {
				return err
			}
		}
		out = append(out, sub)
		return nil
	})
	return out, err
}
