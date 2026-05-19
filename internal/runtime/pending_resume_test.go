package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

type mockPendingResumeStore struct {
	run *events.RunRecord
	err error
}

func (m *mockPendingResumeStore) FindLatestInterruptedRun(_ context.Context) (*events.RunRecord, error) {
	return m.run, m.err
}

func TestFindPendingResume_Found(t *testing.T) {
	now := time.Now().UTC()
	store := &mockPendingResumeStore{
		run: &events.RunRecord{
			RunID: "run_int", SessionID: "sess_1", Input: "interrupted task",
			Status: events.RunStatusInterrupted, CreatedAt: now, UpdatedAt: now,
		},
	}
	info, err := FindPendingResume(context.Background(), store)
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
	store := &mockPendingResumeStore{run: nil}
	info, err := FindPendingResume(context.Background(), store)
	if err != nil {
		t.Fatalf("FindPendingResume: %v", err)
	}
	if info != nil {
		t.Error("expected nil info when no interrupted runs")
	}
}
