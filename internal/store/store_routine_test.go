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
	thread, err := s.LatestRoutineThread(ctx, "night", "wander")
	if err != nil || thread != "wander" {
		t.Fatalf("latest thoughts thread: %q, %v", thread, err)
	}
}
