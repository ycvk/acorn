package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// runProcessesTable is shared with the migration that adds it. It names the
// Acorn process executing each running run; a run's row goes when the run
// stops running.
const runProcessesTable = `CREATE TABLE IF NOT EXISTS run_processes (
    run_id TEXT PRIMARY KEY,
    process TEXT NOT NULL
);`

// processesDir holds one lock file per open store, named after its process.
// The process keeps its file locked until the store closes, and the OS drops
// the lock when the process dies, so a process whose file is missing or
// unlocked has stopped.
const processesDir = "processes"

// addRunProcesses (11 to 12) records which process executes each running run.
// Runs left running before it have no process and count as abandoned.
func addRunProcesses(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, runProcessesTable)
	return err
}

// holdProcessLock creates and locks the lock file of a new process. The file
// is locked before it gets its name, so another process never finds it
// unlocked while this one runs.
func holdProcessLock(dir string) (string, *os.File, error) {
	processes := filepath.Join(dir, processesDir)
	if err := os.MkdirAll(processes, 0o700); err != nil {
		return "", nil, fmt.Errorf("create processes dir: %w", err)
	}
	process := strings.ToLower(rand.Text())
	staged := filepath.Join(processes, "."+process)
	f, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("create process lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return "", nil, errors.Join(fmt.Errorf("lock process lock: %w", err), f.Close(), os.Remove(staged))
	}
	if err := os.Rename(staged, processLockPath(dir, process)); err != nil {
		return "", nil, errors.Join(fmt.Errorf("name process lock: %w", err), f.Close(), os.Remove(staged))
	}
	return process, f, nil
}

func processLockPath(dir, process string) string {
	return filepath.Join(dir, processesDir, process+".lock")
}

// releaseProcessLock removes this process's lock file once the store no
// longer writes.
func (s *Store) releaseProcessLock() error {
	return errors.Join(os.Remove(processLockPath(s.dir, s.process)), s.processLock.Close())
}

// lockStoppedProcess takes the lock file of process if the process has
// stopped. stopped is false while the process still holds it. A run without a
// recorded process predates process tracking, so its process has stopped too.
// release removes the stopped process's file.
func (s *Store) lockStoppedProcess(process string) (release func() error, stopped bool, err error) {
	if process == "" {
		return func() error { return nil }, true, nil
	}
	path := processLockPath(s.dir, process)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return func() error { return nil }, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open lock of process %s: %w", process, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, closeErr
		}
		return nil, false, errors.Join(fmt.Errorf("lock process %s: %w", process, err), closeErr)
	}
	return func() error { return errors.Join(os.Remove(path), f.Close()) }, true, nil
}

// FailAbandonedRuns fails every running run whose process has stopped and
// returns their ids. A process that stops mid-run leaves its runs running,
// which keeps their threads busy; this records how they ended, as a run
// failure would: a run.failed event with reason, the failed status and no
// checkpoint. It also removes the lock files of stopped processes.
func (s *Store) FailAbandonedRuns(ctx context.Context, reason string) ([]string, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("fail abandoned runs requires reason")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.run_id, COALESCE(p.process, '') FROM runs r LEFT JOIN run_processes p ON p.run_id = r.run_id WHERE r.status = ? ORDER BY r.created_at, r.run_id`, string(core.RunStatusRunning))
	if err != nil {
		return nil, fmt.Errorf("list running runs: %w", err)
	}
	runs := map[string][]string{}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var runID, process string
		if err := scan(&runID, &process); err != nil {
			return err
		}
		runs[process] = append(runs[process], runID)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list running runs: %w", err)
	}
	files, err := os.ReadDir(filepath.Join(s.dir, processesDir))
	if err != nil {
		return nil, fmt.Errorf("list process locks: %w", err)
	}
	processes := make([]string, 0, len(runs)+len(files))
	for process := range runs {
		processes = append(processes, process)
	}
	for _, file := range files {
		if process, ok := strings.CutSuffix(file.Name(), ".lock"); ok {
			processes = append(processes, process)
		}
	}
	slices.Sort(processes)
	var failed []string
	for _, process := range slices.Compact(processes) {
		if process == s.process {
			continue
		}
		release, stopped, err := s.lockStoppedProcess(process)
		if err != nil {
			return failed, err
		}
		if !stopped {
			continue
		}
		ended, err := s.failRunsOfStoppedProcess(ctx, process, runs[process], reason)
		failed = append(failed, ended...)
		if err := errors.Join(err, release()); err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// failRunsOfStoppedProcess fails the runs that are still running under the
// stopped process and returns their ids.
func (s *Store) failRunsOfStoppedProcess(ctx context.Context, process string, runIDs []string, reason string) ([]string, error) {
	payload, err := json.Marshal(map[string]any{"error": reason})
	if err != nil {
		return nil, fmt.Errorf("marshal run failure: %w", err)
	}
	now := formatTimestamp(time.Now())
	var failed []string
	err = s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		for _, runID := range runIDs {
			result, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, error_text = ?, finished_at = ? WHERE run_id = ? AND status = ? AND COALESCE((SELECT process FROM run_processes WHERE run_id = ?), '') = ?`,
				string(core.RunStatusFailed), reason, now, runID, string(core.RunStatusRunning), runID, process)
			if err != nil {
				return fmt.Errorf("fail run %s: %w", runID, err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("fail run %s rows affected: %w", runID, err)
			}
			if affected == 0 {
				continue
			}
			for _, statement := range []struct {
				query string
				args  []any
			}{
				{`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, 'run.failed', ?, ?)`, []any{runID, string(payload), now}},
				{`DELETE FROM run_processes WHERE run_id = ?`, []any{runID}},
				{`DELETE FROM agent_checkpoints WHERE checkpoint_id = ?`, []any{runID}},
			} {
				if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
					return fmt.Errorf("fail run %s: %w", runID, err)
				}
			}
			failed = append(failed, runID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return failed, nil
}
