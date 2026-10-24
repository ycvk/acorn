package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var _ core.RoutineStore = (*Store)(nil)

func (s *Store) ClaimRoutine(ctx context.Context, routine, slot string, at time.Time) error {
	if routine == "" || slot == "" || at.IsZero() {
		return errors.New("claim routine: routine, slot and time are required")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO routine_runs (routine, slot, created_at) VALUES (?, ?, ?) ON CONFLICT(routine, slot) DO NOTHING`, routine, slot, formatTimestamp(at))
	if err != nil {
		return fmt.Errorf("claim routine %s %s: %w", routine, slot, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("claim routine rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s %s", core.ErrRoutineTaken, routine, slot)
	}
	return nil
}

func (s *Store) ReleaseRoutine(ctx context.Context, routine, slot string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM routine_runs WHERE routine = ? AND slot = ? AND run_id = ''`, routine, slot)
	if err != nil {
		return fmt.Errorf("release routine %s %s: %w", routine, slot, err)
	}
	return nil
}

func (s *Store) SetRoutineRun(ctx context.Context, routine, slot, threadID, runID string) error {
	if threadID == "" || runID == "" {
		return errors.New("set routine run: thread and run are required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE routine_runs SET thread_id = ?, run_id = ? WHERE routine = ? AND slot = ?`, threadID, runID, routine, slot)
	if err != nil {
		return fmt.Errorf("set routine run %s %s: %w", routine, slot, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set routine run rows affected: %w", err)
	}
	if n != 1 {
		return errors.New("set routine run: routine slot must be claimed")
	}
	return nil
}

func (s *Store) LatestRoutineThread(ctx context.Context, routine string) (string, error) {
	if routine == "" {
		return "", errors.New("latest routine thread: routine is required")
	}
	var thread string
	err := s.db.QueryRowContext(ctx, `SELECT thread_id FROM routine_runs WHERE run_id <> '' AND routine = ? ORDER BY created_at DESC, slot DESC LIMIT 1`, routine).Scan(&thread)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest routine thread: %w", err)
	}
	return thread, nil
}

func (s *Store) LastRoutineAt(ctx context.Context, routine, beforeSlot string) (time.Time, error) {
	var at string
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM routine_runs WHERE routine = ? AND slot < ? AND run_id <> '' ORDER BY slot DESC LIMIT 1`, routine, beforeSlot).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("last routine time: %w", err)
	}
	return parseTimestamp(fixedTimestampLayout, at, "routine_runs.created_at")
}

// LatestRoutineOutput is the reply of routine's newest succeeded run; zero
// when none succeeded or its thread was deleted with the run.
func (s *Store) LatestRoutineOutput(ctx context.Context, routine string) (core.RoutineOutput, error) {
	if routine == "" {
		return core.RoutineOutput{}, errors.New("latest routine output: routine is required")
	}
	var out core.RoutineOutput
	var finished string
	err := s.db.QueryRowContext(ctx, `SELECT rr.slot, rr.thread_id, rr.run_id, r.output_text, r.finished_at FROM routine_runs rr JOIN runs r ON r.run_id = rr.run_id
		WHERE rr.routine = ? AND r.status = ? AND r.output_text <> '' ORDER BY rr.created_at DESC, rr.slot DESC LIMIT 1`, routine, string(core.RunStatusSucceeded)).
		Scan(&out.Slot, &out.ThreadID, &out.RunID, &out.Text, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return core.RoutineOutput{}, nil
	}
	if err != nil {
		return core.RoutineOutput{}, fmt.Errorf("latest routine output: %w", err)
	}
	out.FinishedAt, err = parseTimestamp(time.RFC3339Nano, finished, "runs.finished_at")
	return out, err
}
