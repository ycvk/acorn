package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestRoutineClaimsAndSuccessfulWindow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-05", at); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-05", at); !errors.Is(err, core.ErrRoutineTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.SetRoutineRun(ctx, "briefing", "2026-10-05", "brief", "r1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-06", at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	last, err := s.LastRoutineAt(ctx, "briefing", "2026-10-07")
	if err != nil || !last.Equal(at) {
		t.Fatalf("window after skipped slot: %s, %v", last, err)
	}
	if err := s.ReleaseRoutine(ctx, "briefing", "2026-10-06"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-06", at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"night", "wander"} {
		if err := s.ClaimRoutine(ctx, name, "2026-10-05", at.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := s.SetRoutineRun(ctx, name, "2026-10-05", name, "run"); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := s.LatestRoutineThread(ctx, "night")
	if err != nil || thread != "night" {
		t.Fatalf("latest thoughts thread: %q, %v", thread, err)
	}
	night, err := s.LatestRoutineRun(ctx, "night")
	if err != nil || night.Slot != "2026-10-05" || night.RunID != "run" || night.ThreadID != "night" || !night.CreatedAt.Equal(at) {
		t.Fatalf("latest night run: %+v, %v", night, err)
	}
	if none, err := s.LatestRoutineRun(ctx, "never"); err != nil || none.RunID != "" {
		t.Fatalf("unknown routine: %+v, %v", none, err)
	}
}

func TestLatestRoutineOutputSkipsRunsWithoutAReply(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if none, err := s.LatestRoutineOutput(ctx, "briefing"); err != nil || none.RunID != "" {
		t.Fatalf("before any briefing: %+v, %v", none, err)
	}
	for i, run := range []struct {
		id, slot, output string
		status           core.RunStatus
	}{
		{"run_1", "2026-10-05", "## 今天\n交房租", core.RunStatusSucceeded},
		{"run_2", "2026-10-06", "", core.RunStatusSucceeded},
		{"run_3", "2026-10-07", "", core.RunStatusFailed},
	} {
		if err := s.ClaimRoutine(ctx, "briefing", run.slot, at.Add(time.Duration(i)*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateRun(ctx, core.RunCreateParams{RunID: run.id, Input: "[briefing " + run.slot + "]"}); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishRun(ctx, run.id, run.status, run.output, ""); err != nil {
			t.Fatal(err)
		}
		if err := s.SetRoutineRun(ctx, "briefing", run.slot, "thread_briefings", run.id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LatestRoutineOutput(ctx, "briefing")
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "run_1" || got.Slot != "2026-10-05" || got.ThreadID != "thread_briefings" || got.Text != "## 今天\n交房租" || got.FinishedAt.IsZero() {
		t.Fatalf("latest briefing output = %+v", got)
	}
}
