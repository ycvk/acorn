package runtime

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func openPendingResumeTestStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	s, err := storesqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestFindPendingResume_Found(t *testing.T) {
	ctx := context.Background()
	store := openPendingResumeTestStore(t)
	if _, err := store.CreateSession(ctx, "sess_1", "test"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_int", "sess_1", 1, "interrupted task", "cp_1"); err != nil {
		t.Fatalf("CreateRunWithSession: %v", err)
	}
	if err := store.FinishRunContext(ctx, "run_int", events.RunStatusInterrupted, "", ""); err != nil {
		t.Fatalf("FinishRunContext: %v", err)
	}

	info, err := FindPendingResume(ctx, store)
	if err != nil {
		t.Fatalf("FindPendingResume: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.RunID != "run_int" {
		t.Errorf("run_id: got %q, want %q", info.RunID, "run_int")
	}
	if info.SessionID != "sess_1" {
		t.Errorf("session_id: got %q, want %q", info.SessionID, "sess_1")
	}
}

func TestFindPendingResume_None(t *testing.T) {
	store := openPendingResumeTestStore(t)
	info, err := FindPendingResume(context.Background(), store)
	if err != nil {
		t.Fatalf("FindPendingResume: %v", err)
	}
	if info != nil {
		t.Error("expected nil info when no interrupted runs")
	}
}
