package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// addMemoryJobFailures (27 to 28) records when each memory job was given up
// on. The table is rebuilt from memoryJobsTable so its stored definition
// matches a new database; a job given up on earlier takes the retry time its
// last failure set, minutes after the failure.
func addMemoryJobFailures(ctx context.Context, tx *sql.Tx) error {
	columns := "id, operation, object_id, version, processor_version, state, token, epoch, attempts, cursor, lease_until, available_at, error, created_at, completed_at"
	for _, statement := range []string{
		`CREATE TEMP TABLE memory_jobs_v27 AS SELECT ` + columns + ` FROM memory_jobs`,
		`DROP TABLE memory_jobs`,
		memoryJobsTable,
		`INSERT INTO memory_jobs(` + columns + `, failed_at) SELECT ` + columns + `, CASE state WHEN 'failed' THEN available_at ELSE '' END FROM memory_jobs_v27`,
		`DROP TABLE memory_jobs_v27`,
		`CREATE INDEX IF NOT EXISTS idx_memory_jobs_ready ON memory_jobs(state,available_at,lease_until,id)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_jobs_completed ON memory_jobs(completed_at)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func enqueueMemoryJob(ctx context.Context, q memorySQL, operation, id, version string, now time.Time) error {
	_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO memory_jobs(operation,object_id,version,available_at,created_at) VALUES(?,?,?,?,?)`, operation, id, version, formatTimestamp(now), formatTimestamp(now))
	return err
}

func (s *Store) MemoryEpoch(ctx context.Context) (int64, error) { return memoryEpoch(ctx, s.db) }

func memoryEpoch(ctx context.Context, q memorySQL) (int64, error) {
	var epoch int64
	err := q.QueryRowContext(ctx, `SELECT epoch FROM memory_state WHERE id=1`).Scan(&epoch)
	return epoch, err
}

func (s *Store) ClaimMemoryJob(ctx context.Context, now time.Time, lease time.Duration, operation string) (*core.MemoryJob, error) {
	if now.IsZero() || lease <= 0 {
		return nil, errors.New("memory job requires now and a positive lease")
	}
	var result *core.MemoryJob
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		jobs, err := readyMemoryJobs(ctx, tx, now, operation, 1)
		if err != nil || len(jobs) == 0 {
			return err
		}
		job, err := claimMemoryJob(ctx, tx, jobs[0], now, lease)
		if err == nil {
			result = &job
		}
		return err
	})
	return result, err
}

// ReadyMemoryJobs lists up to limit jobs of operation a worker could claim
// now, oldest first, without claiming them.
func (s *Store) ReadyMemoryJobs(ctx context.Context, now time.Time, operation string, limit int) ([]core.MemoryJob, error) {
	if now.IsZero() || operation == "" || limit <= 0 {
		return nil, errors.New("ready memory jobs require now, operation and a positive limit")
	}
	return readyMemoryJobs(ctx, s.db, now, operation, limit)
}

// ClaimMemoryJobs claims the listed jobs that are still ready and skips the
// others, which another worker took or which changed state.
func (s *Store) ClaimMemoryJobs(ctx context.Context, ids []int64, now time.Time, lease time.Duration) ([]core.MemoryJob, error) {
	if now.IsZero() || lease <= 0 {
		return nil, errors.New("memory jobs require now and a positive lease")
	}
	var claimed []core.MemoryJob
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			var job core.MemoryJob
			err := tx.QueryRowContext(ctx, `SELECT id,operation,object_id,version,token,attempts,cursor,epoch FROM memory_jobs WHERE id=? AND `+memoryJobReady, id, formatTimestamp(now), formatTimestamp(now)).Scan(&job.ID, &job.Operation, &job.ObjectID, &job.Version, &job.Token, &job.Attempts, &job.Cursor, &job.Epoch)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			job, err = claimMemoryJob(ctx, tx, job, now, lease)
			if err != nil {
				return err
			}
			claimed = append(claimed, job)
		}
		return nil
	})
	return claimed, err
}

const memoryJobReady = `((state='pending' AND available_at<=?) OR (state='running' AND lease_until<=?))`

func readyMemoryJobs(ctx context.Context, q memorySQL, now time.Time, operation string, limit int) ([]core.MemoryJob, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,operation,object_id,version,token,attempts,cursor,epoch FROM memory_jobs
		WHERE `+memoryJobReady+` AND (?='' OR operation=?) ORDER BY id LIMIT ?`, formatTimestamp(now), formatTimestamp(now), operation, operation, limit)
	if err != nil {
		return nil, err
	}
	var jobs []core.MemoryJob
	err = scanRows(rows, func(scan func(...any) error) error {
		var job core.MemoryJob
		if err := scan(&job.ID, &job.Operation, &job.ObjectID, &job.Version, &job.Token, &job.Attempts, &job.Cursor, &job.Epoch); err != nil {
			return err
		}
		jobs = append(jobs, job)
		return nil
	})
	return jobs, err
}

func claimMemoryJob(ctx context.Context, q memorySQL, job core.MemoryJob, now time.Time, lease time.Duration) (core.MemoryJob, error) {
	previousEpoch := job.Epoch
	var err error
	job.Epoch, err = memoryEpoch(ctx, q)
	if err != nil {
		return job, err
	}
	if previousEpoch != job.Epoch {
		job.Cursor = 0
	}
	job.Token++
	job.Attempts++
	job.State = "running"
	job.LeaseUntil = now.Add(lease)
	_, err = q.ExecContext(ctx, `UPDATE memory_jobs SET state='running',token=?,attempts=?,epoch=?,lease_until=?,cursor=? WHERE id=?`, job.Token, job.Attempts, job.Epoch, formatTimestamp(job.LeaseUntil), job.Cursor, job.ID)
	return job, err
}

func validateMemoryLease(ctx context.Context, q memorySQL, job core.MemoryJob, now time.Time) error {
	epoch, err := memoryEpoch(ctx, q)
	if err != nil {
		return err
	}
	if epoch != job.Epoch {
		return fmt.Errorf("%w: processing exclusion version changed", core.ErrMemoryExcluded)
	}
	var state, until string
	var token int64
	if err := q.QueryRowContext(ctx, `SELECT state,token,lease_until FROM memory_jobs WHERE id=?`, job.ID).Scan(&state, &token, &until); err != nil {
		return err
	}
	if state != "running" || token != job.Token || until <= formatTimestamp(now) {
		return fmt.Errorf("%w: job %d", core.ErrMemoryLeaseLost, job.ID)
	}
	return nil
}

func finishMemoryJob(ctx context.Context, q memorySQL, job core.MemoryJob, reason string, now time.Time) error {
	if err := validateMemoryLease(ctx, q, job, now); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `UPDATE memory_jobs SET state='done',completed_at=?,lease_until='',error=? WHERE id=? AND token=?`, formatTimestamp(now), reason, job.ID, job.Token)
	return err
}

func (s *Store) FinishMemoryJob(ctx context.Context, job core.MemoryJob, reason string, now time.Time) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error { return finishMemoryJob(ctx, tx, job, reason, now) })
}

func (s *Store) FailMemoryJob(ctx context.Context, job core.MemoryJob, reason string, now time.Time) error {
	state, failedAt := "pending", ""
	if job.Attempts >= 3 {
		state, failedAt = "failed", formatTimestamp(now)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE memory_jobs SET state=?,error=?,available_at=?,failed_at=?,lease_until='' WHERE id=? AND token=? AND state='running'`, state, reason, formatTimestamp(now.Add(time.Minute*time.Duration(job.Attempts))), failedAt, job.ID, job.Token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return core.ErrMemoryLeaseLost
	}
	return nil
}

// Completed jobs accumulate; every term reads only an index range.
const memoryJobStatusSQL = `SELECT (SELECT COUNT(*) FROM memory_jobs WHERE state IN ('pending','running')),(SELECT COUNT(*) FROM memory_jobs WHERE state='failed'),
COALESCE((SELECT MIN(created_at) FROM memory_jobs WHERE state IN ('pending','running')),''),COALESCE((SELECT MAX(completed_at) FROM memory_jobs),'')`

func (s *Store) MemoryProcessingStatus(ctx context.Context) (core.MemoryProcessingStatus, error) {
	var status core.MemoryProcessingStatus
	var oldest, last string
	err := s.read.QueryRowContext(ctx, memoryJobStatusSQL).Scan(&status.Pending, &status.Failed, &oldest, &last)
	if err != nil {
		return status, err
	}
	if oldest != "" {
		if status.Oldest, err = time.Parse(time.RFC3339Nano, oldest); err != nil {
			return status, err
		}
	}
	if last != "" {
		if status.LastCompleted, err = time.Parse(time.RFC3339Nano, last); err != nil {
			return status, err
		}
	}
	err = s.read.QueryRowContext(ctx, `SELECT error FROM memory_jobs WHERE state='failed' ORDER BY id DESC LIMIT 1`).Scan(&status.LastError)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return status, err
	}
	status.Index, err = s.MemoryIndex(ctx)
	if errors.Is(err, core.ErrMemoryNotFound) {
		err = nil
	}
	return status, err
}

// failingMemoryWorkSQL lists each operation's jobs given up on after its
// last completed job, with the first such failure and the newest error.
const failingMemoryWorkSQL = `WITH completed AS (SELECT operation, MAX(completed_at) AS at FROM memory_jobs WHERE state='done' GROUP BY operation)
SELECT f.operation, COUNT(*), MIN(f.failed_at),
 (SELECT e.error FROM memory_jobs e WHERE e.state='failed' AND e.operation=f.operation ORDER BY e.failed_at DESC, e.id DESC LIMIT 1)
FROM memory_jobs f LEFT JOIN completed c ON c.operation=f.operation
WHERE f.state='failed' AND f.failed_at > COALESCE(c.at,'')
GROUP BY f.operation ORDER BY f.operation`

func (s *Store) FailingMemoryWork(ctx context.Context) ([]core.FailingWork, error) {
	rows, err := s.read.QueryContext(ctx, failingMemoryWorkSQL)
	if err != nil {
		return nil, fmt.Errorf("failing memory work: %w", err)
	}
	var failing []core.FailingWork
	if err := scanRows(rows, func(scan func(...any) error) error {
		var work core.FailingWork
		var since string
		if err := scan(&work.Kind, &work.Failed, &since, &work.LastError); err != nil {
			return err
		}
		var err error
		work.Since, err = parseTimestamp(fixedTimestampLayout, since, "memory_jobs.failed_at")
		failing = append(failing, work)
		return err
	}); err != nil {
		return nil, fmt.Errorf("failing memory work: %w", err)
	}
	return failing, nil
}

func (s *Store) RecordMemoryUsage(ctx context.Context, u core.MemoryUsage) error {
	if u.ID == "" || u.CreatedAt.IsZero() || u.Budget == "" || u.InputTokens < 0 || u.OutputTokens < 0 {
		return errors.New("memory usage requires id, budget, created_at and nonnegative tokens")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO memory_usage(id,operation,job_id,run_id,budget,model,input_tokens,output_tokens,reported,created_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,reported=excluded.reported`, u.ID, u.Operation, u.JobID, u.RunID, u.Budget, u.Model, u.InputTokens, u.OutputTokens, u.Reported, formatTimestamp(u.CreatedAt))
	return err
}

func (s *Store) MemoryUsageSince(ctx context.Context, since time.Time, budget string) (int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens+output_tokens),0) FROM memory_usage WHERE created_at>=? AND (?='' OR budget=?)`, formatTimestamp(since), budget, budget).Scan(&total)
	return total, err
}

// DeferMemoryJob leaves budget-limited work available in the next local day.
func (s *Store) DeferMemoryJob(ctx context.Context, job core.MemoryJob, reason string, available time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE memory_jobs SET state='pending',error=?,available_at=?,attempts=attempts-1,lease_until='' WHERE id=? AND token=? AND state='running'`, reason, formatTimestamp(available), job.ID, job.Token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return core.ErrMemoryLeaseLost
	}
	return nil
}

func (s *Store) ReserveMemoryUsage(ctx context.Context, u core.MemoryUsage, limit int, since time.Time) error {
	if u.ID == "" || u.CreatedAt.IsZero() || u.Budget == "" || u.InputTokens < 0 || u.OutputTokens < 0 {
		return errors.New("memory usage reservation requires valid id, time, budget and tokens")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var total int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens+output_tokens),0) FROM memory_usage WHERE created_at>=? AND budget=?`, formatTimestamp(since), u.Budget).Scan(&total); err != nil {
			return err
		}
		if u.Budget == "autonomous" {
			var runTokens int
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(json_extract(payload_json,'$.total_tokens')),0) FROM events e WHERE kind=? AND created_at>=? AND EXISTS(SELECT 1 FROM events w WHERE w.run_id=e.run_id AND w.kind=?)`, core.EventModelUsage, formatTimestamp(since), core.EventWakeFired).Scan(&runTokens); err != nil {
				return err
			}
			total += runTokens
		}
		if limit > 0 && total+u.InputTokens+u.OutputTokens > limit {
			return core.ErrMemoryBudget
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO memory_usage(id,operation,job_id,run_id,budget,model,input_tokens,output_tokens,reported,created_at) VALUES(?,?,?,?,?,?,?,?,0,?)`, u.ID, u.Operation, u.JobID, u.RunID, u.Budget, u.Model, u.InputTokens, u.OutputTokens, formatTimestamp(u.CreatedAt))
		return err
	})
}
