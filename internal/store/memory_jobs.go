package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

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
		var job core.MemoryJob
		err := tx.QueryRowContext(ctx, `SELECT id,operation,object_id,version,token,attempts,cursor,epoch FROM memory_jobs
		WHERE ((state='pending' AND available_at<=?) OR (state='running' AND lease_until<=?)) AND (?='' OR operation=?) ORDER BY id LIMIT 1`, formatTimestamp(now), formatTimestamp(now), operation, operation).Scan(&job.ID, &job.Operation, &job.ObjectID, &job.Version, &job.Token, &job.Attempts, &job.Cursor, &job.Epoch)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		previousEpoch := job.Epoch
		job.Epoch, err = memoryEpoch(ctx, tx)
		if err != nil {
			return err
		}
		if previousEpoch != job.Epoch {
			job.Cursor = 0
		}
		job.Token++
		job.Attempts++
		job.State = "running"
		job.LeaseUntil = now.Add(lease)
		_, err = tx.ExecContext(ctx, `UPDATE memory_jobs SET state='running',token=?,attempts=?,epoch=?,lease_until=?,cursor=? WHERE id=?`, job.Token, job.Attempts, job.Epoch, formatTimestamp(job.LeaseUntil), job.Cursor, job.ID)
		if err == nil {
			result = &job
		}
		return err
	})
	return result, err
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
	state := "pending"
	if job.Attempts >= 3 {
		state = "failed"
	}
	result, err := s.db.ExecContext(ctx, `UPDATE memory_jobs SET state=?,error=?,available_at=?,lease_until='' WHERE id=? AND token=? AND state='running'`, state, reason, formatTimestamp(now.Add(time.Minute*time.Duration(job.Attempts))), job.ID, job.Token)
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
