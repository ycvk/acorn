package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func seedRunningRun(t *testing.T, s *Store, sessionID, runID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, sessionID, sessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: runID, SessionID: sessionID, TurnIndex: 1, Input: "hello"}); err != nil {
		t.Fatal(err)
	}
}

// crash stops s the way a killed process does: the database and locks are
// released, and its process lock file stays behind.
func crash(t *testing.T, s *Store) {
	t.Helper()
	if err := errors.Join(s.read.Close(), s.db.Close(), s.processLock.Close(), s.lock.Close()); err != nil {
		t.Fatal(err)
	}
}

// TestFailAbandonedRunsFailsRunsOfStoppedProcesses covers runs left running by
// a process that stopped mid-run: they keep their threads busy until the next
// start fails them, while a run of a process that still runs is left alone.
func TestFailAbandonedRunsFailsRunsOfStoppedProcesses(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	open := func() *Store {
		t.Helper()
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}

	closed := open()
	seedRunningRun(t, closed, "thread_closed", "run_closed")
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	crashed := open()
	seedRunningRun(t, crashed, "thread_crashed", "run_crashed")
	crashedLock := processLockPath(dir, crashed.process)
	crash(t, crashed)
	live := open()
	seedRunningRun(t, live, "thread_live", "run_live")
	if _, err := live.CreateSession(ctx, "thread_untracked", "untracked"); err != nil {
		t.Fatal(err)
	}
	if _, err := live.db.ExecContext(ctx, `INSERT INTO runs(run_id, session_id, turn_index, status, input_text, created_at) VALUES('run_untracked', 'thread_untracked', 1, 'running', 'hello', '2026-10-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	current := open()
	if err := current.DeleteThread(ctx, "thread_crashed"); !errors.Is(err, core.ErrThreadBusy) {
		t.Fatalf("delete thread before recovery = %v, want ErrThreadBusy", err)
	}
	failed, err := current.FailAbandonedRuns(ctx, "acorn stopped")
	if err != nil {
		t.Fatalf("fail abandoned runs: %v", err)
	}
	slices.Sort(failed)
	if want := []string{"run_closed", "run_crashed", "run_untracked"}; !slices.Equal(failed, want) {
		t.Fatalf("failed runs = %v, want %v", failed, want)
	}
	for _, runID := range failed {
		run, err := current.LoadRun(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != core.RunStatusFailed || run.Error != "acorn stopped" || run.FinishedAt.IsZero() {
			t.Fatalf("run %s = %s %q finished %v, want failed with the reason", runID, run.Status, run.Error, run.FinishedAt)
		}
		events, err := current.LoadEvents(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := events[len(events)-1].Payload.(map[string]any)
		if events[len(events)-1].Kind != "run.failed" || payload["error"] != "acorn stopped" {
			t.Fatalf("run %s last event = %+v, want run.failed", runID, events[len(events)-1])
		}
	}
	if _, err := os.Stat(crashedLock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crashed process lock = %v, want removed", err)
	}
	if err := current.DeleteThread(ctx, "thread_crashed"); err != nil {
		t.Fatalf("delete thread after recovery: %v", err)
	}
	run, err := current.LoadRun(ctx, "run_live")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != core.RunStatusRunning {
		t.Fatalf("live process's run = %s, want running", run.Status)
	}
	if again, err := current.FailAbandonedRuns(ctx, "acorn stopped"); err != nil || len(again) != 0 {
		t.Fatalf("second recovery = %v, %v; want nothing", again, err)
	}

	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	if failed, err := current.FailAbandonedRuns(ctx, "acorn stopped"); err != nil || !slices.Equal(failed, []string{"run_live"}) {
		t.Fatalf("recovery after the live process stopped = %v, %v; want run_live", failed, err)
	}
}

// TestResumedRunBelongsToTheResumingProcess covers a run interrupted in one
// process and resumed in another: while the resuming process runs, the run is
// its own.
func TestResumedRunBelongsToTheResumingProcess(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedRunningRun(t, first, "thread_1", "run_1")
	if err := first.MarkInterrupted(ctx, "run_1", ""); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	resuming, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resuming.Close() })
	if err := resuming.ResumeInterruptedRun(ctx, "run_1"); err != nil {
		t.Fatal(err)
	}

	current, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = current.Close() })
	if failed, err := current.FailAbandonedRuns(ctx, "acorn stopped"); err != nil || len(failed) != 0 {
		t.Fatalf("recovery while the resuming process runs = %v, %v; want nothing", failed, err)
	}
	if err := resuming.Close(); err != nil {
		t.Fatal(err)
	}
	if failed, err := current.FailAbandonedRuns(ctx, "acorn stopped"); err != nil || !slices.Equal(failed, []string{"run_1"}) {
		t.Fatalf("recovery after the resuming process stopped = %v, %v; want run_1", failed, err)
	}
}

// TestCloseRemovesProcessLock covers a store that closes normally: its process
// lock file goes with it.
func TestCloseRemovesProcessLock(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := processLockPath(dir, s.process)
	if filepath.Dir(path) != filepath.Join(dir, processesDir) {
		t.Fatalf("process lock %s is outside %s", path, processesDir)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("process lock after close = %v, want removed", err)
	}
}
