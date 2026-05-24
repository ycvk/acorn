package runstream

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

func TestDeriveSessionState(t *testing.T) {
	cases := []struct {
		name                string
		status              events.RunStatus
		hasDegradedProvider bool
		want                SessionState
	}{
		{"nil run", "", false, SessionStateNew},
		{"succeeded", events.RunStatusSucceeded, false, SessionStateCompleted},
		{"succeeded degraded", events.RunStatusSucceeded, true, SessionStateDegraded},
		{"failed", events.RunStatusFailed, false, SessionStateFailed},
		{"running", events.RunStatusRunning, false, SessionStateRunning},
		{"interrupted", events.RunStatusInterrupted, false, SessionStateInterrupted},
		{"interrupted degraded", events.RunStatusInterrupted, true, SessionStateDegraded},
		{"unknown status", events.RunStatus("weird"), false, SessionStateDegraded},
		{"unknown degraded", events.RunStatus("weird"), true, SessionStateDegraded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var run *events.RunRecord
			if tc.status != "" {
				run = &events.RunRecord{Status: tc.status}
			}
			got := DeriveSessionState(run, tc.hasDegradedProvider)
			if got != tc.want {
				t.Fatalf("DeriveSessionState(%q, %v) = %q, want %q", tc.status, tc.hasDegradedProvider, got, tc.want)
			}
		})
	}
}

func TestStreamSinkContext(t *testing.T) {
	ctx := context.Background()

	if got := StreamSinkFromContext(ctx); got != nil {
		t.Fatalf("StreamSinkFromContext(nil sink) = %v, want nil", got)
	}

	if got := StreamSinkFromContext(nil); got != nil { //nolint:staticcheck // Exercise the nil-context guard intentionally.
		t.Fatalf("StreamSinkFromContext(nil ctx) = %v, want nil", got)
	}

	var called bool
	sink := StreamSink(func(item StreamItem) error {
		called = true
		return nil
	})

	ctx = WithStreamSink(ctx, sink)
	got := StreamSinkFromContext(ctx)
	if got == nil {
		t.Fatalf("StreamSinkFromContext = nil, want non-nil")
	}

	_ = got(StreamItem{})
	if !called {
		t.Fatalf("sink was not called")
	}

	// WithStreamSink with nil returns ctx unchanged, so previous sink remains
	ctxWithNil := WithStreamSink(ctx, nil)
	if got := StreamSinkFromContext(ctxWithNil); got == nil {
		t.Fatalf("StreamSinkFromContext(after nil) = nil, want previous sink")
	}
}

func TestRunError(t *testing.T) {
	err := NewRunError("something went wrong")
	if err == nil {
		t.Fatalf("NewRunError = nil")
	}
	if err.Error() != "something went wrong" {
		t.Fatalf("error message = %q, want %q", err.Error(), "something went wrong")
	}
}

func TestPendingResumeInfoFromStore(t *testing.T) {
	now := time.Now()
	store := &stubPendingResumeStore{
		run: &events.RunRecord{
			RunID:     "run_1",
			SessionID: "session_1",
			Input:     "hello",
			CreatedAt: now,
		},
	}

	info, err := FindPendingResume(context.Background(), store)
	if err != nil {
		t.Fatalf("FindPendingResume: %v", err)
	}
	if info == nil {
		t.Fatalf("FindPendingResume = nil, want non-nil")
	}
	if info.RunID != "run_1" {
		t.Fatalf("run_id = %q, want %q", info.RunID, "run_1")
	}
	if info.SessionID != "session_1" {
		t.Fatalf("session_id = %q, want %q", info.SessionID, "session_1")
	}
	if info.Input != "hello" {
		t.Fatalf("input = %q, want %q", info.Input, "hello")
	}

	emptyStore := &stubPendingResumeStore{run: nil}
	info, err = FindPendingResume(context.Background(), emptyStore)
	if err != nil {
		t.Fatalf("FindPendingResume empty: %v", err)
	}
	if info != nil {
		t.Fatalf("FindPendingResume = %v, want nil", info)
	}
}

func TestPendingResumeStoreError(t *testing.T) {
	store := &stubPendingResumeStore{err: context.Canceled}
	_, err := FindPendingResume(context.Background(), store)
	if err == nil {
		t.Fatalf("FindPendingResume = nil, want error")
	}
}

type stubPendingResumeStore struct {
	run *events.RunRecord
	err error
}

func (s *stubPendingResumeStore) FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error) {
	return s.run, s.err
}
