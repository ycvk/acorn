package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

type pendingResumeTestStore struct {
	runs []events.RunRecord
}

func (s *pendingResumeTestStore) FindLatestInterruptedRun(_ context.Context) (*events.RunRecord, error) {
	latestIndex := -1
	for i := range s.runs {
		if s.runs[i].Status != events.RunStatusInterrupted {
			continue
		}
		if latestIndex == -1 || s.runs[i].CreatedAt.After(s.runs[latestIndex].CreatedAt) {
			latestIndex = i
		}
	}
	if latestIndex == -1 {
		return nil, nil
	}
	return &s.runs[latestIndex], nil
}

func TestFindPendingResume_Found(t *testing.T) {
	ctx := context.Background()
	store := &pendingResumeTestStore{
		runs: []events.RunRecord{
			{
				RunID:     "run_int",
				SessionID: "sess_1",
				Status:    events.RunStatusInterrupted,
				Input:     "interrupted task",
				CreatedAt: time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC),
			},
		},
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
	store := &pendingResumeTestStore{}
	info, err := FindPendingResume(context.Background(), store)
	if err != nil {
		t.Fatalf("FindPendingResume: %v", err)
	}
	if info != nil {
		t.Error("expected nil info when no interrupted runs")
	}
}
