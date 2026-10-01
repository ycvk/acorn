package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
)

// resumeReadyStore models one interrupted run with tool_approval actions.
type resumeReadyStore struct {
	unimplementedStore
	mu       sync.Mutex
	run      core.RunRecord
	actions  map[string]core.PendingActionRecord
	events   []core.EventRecord
	finished []core.RunStatus
}

func newResumeReadyStore(status core.RunStatus, actionIDs ...string) *resumeReadyStore {
	store := &resumeReadyStore{
		run:     core.RunRecord{RunID: "run_1", SessionID: "thread_1", Status: status},
		actions: map[string]core.PendingActionRecord{},
	}
	contexts := make([]map[string]any, 0, len(actionIDs))
	for i, id := range actionIDs {
		store.actions[id] = core.PendingActionRecord{
			ActionID: id, RunID: "run_1", Kind: core.PendingActionKindToolApproval,
			Status: core.PendingActionStatusPending, PayloadJSON: `{"message":"m"}`,
			CreatedAt: time.Date(2026, 10, 2, 9, 0, i, 0, time.UTC),
		}
		contexts = append(contexts, map[string]any{
			"id": "interrupt_" + id, "is_root_cause": true,
			"info": map[string]any{"kind": "tool_approval", "action_id": id},
		})
	}
	store.events = []core.EventRecord{{RunID: "run_1", Sequence: 1, Kind: "run.interrupted", Payload: map[string]any{
		"interrupt": map[string]any{"contexts": contexts},
	}}}
	return store
}

func (s *resumeReadyStore) LoadRun(context.Context, string) (*core.RunRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.run
	return &run, nil
}

func (s *resumeReadyStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]core.EventRecord(nil), s.events...), nil
}

func (s *resumeReadyStore) ListPendingActionsByRun(context.Context, string) ([]core.PendingActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.PendingActionRecord, 0, len(s.actions))
	for _, action := range s.actions {
		out = append(out, action)
	}
	return out, nil
}

func (s *resumeReadyStore) LoadPendingAction(_ context.Context, id string) (*core.PendingActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, ok := s.actions[id]
	if !ok {
		return nil, core.ErrPendingActionNotFound
	}
	return &action, nil
}

func (s *resumeReadyStore) DecidePendingAction(_ context.Context, id string, status core.PendingActionStatus, decisionJSON string) (*core.PendingActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action := s.actions[id]
	action.Status = status
	action.DecisionJSON = decisionJSON
	s.actions[id] = action
	return &action, nil
}

func (s *resumeReadyStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := core.EventRecord{RunID: runID, Sequence: int64(len(s.events) + 1), Kind: kind, Payload: payload}
	s.events = append(s.events, record)
	return record, nil
}

func (s *resumeReadyStore) FinishRun(_ context.Context, _ string, status core.RunStatus, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.run.Status = status
	s.finished = append(s.finished, status)
	return nil
}

type resumeRecorder struct {
	calls   atomic.Int32
	targets chan map[string]any
	result  *runtime.Result
	err     error
}

func newResumeRecorder() *resumeRecorder {
	return &resumeRecorder{targets: make(chan map[string]any, 4), result: &runtime.Result{RunID: "run_1", Status: core.RunStatusSucceeded}}
}

func (r *resumeRecorder) resume(_ context.Context, _ string, targets map[string]any, _ core.StreamSink) (*runtime.Result, error) {
	r.calls.Add(1)
	r.targets <- targets
	return r.result, r.err
}

func (r *resumeRecorder) waitTargets(t *testing.T) map[string]any {
	t.Helper()
	select {
	case targets := <-r.targets:
		return targets
	case <-time.After(5 * time.Second):
		t.Fatal("resume was not called")
		return nil
	}
}

func TestDecideResumesOnlyAfterLastPendingAction(t *testing.T) {
	store := newResumeReadyStore(core.RunStatusInterrupted, "a1", "a2")
	recorder := newResumeRecorder()
	resumer := NewRunResumeService(store).WithResume(recorder.resume)
	svc := NewPendingActionService(store).WithResumer(resumer)
	ctx := context.Background()

	if _, err := svc.Decide(ctx, "a1", PendingActionDecisionInput{Decision: "accept"}); err != nil {
		t.Fatalf("decide a1: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if recorder.calls.Load() != 0 {
		t.Fatal("resumed while another action was still pending")
	}
	if _, err := svc.Decide(ctx, "a2", PendingActionDecisionInput{Decision: "decline"}); err != nil {
		t.Fatalf("decide a2: %v", err)
	}
	targets := recorder.waitTargets(t)
	if len(targets) != 2 {
		t.Fatalf("targets = %v, want both interrupts", targets)
	}
	if got := targets["interrupt_a2"].(map[string]any)["action"]; got != "decline" {
		t.Fatalf("a2 decision = %v", got)
	}
}

func TestResumeIfReadyIgnoresRunningRun(t *testing.T) {
	store := newResumeReadyStore(core.RunStatusRunning)
	recorder := newResumeRecorder()
	if err := NewRunResumeService(store).WithResume(recorder.resume).ResumeIfReady(context.Background(), "run_1"); err != nil {
		t.Fatalf("resume if ready: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if recorder.calls.Load() != 0 {
		t.Fatal("a running run (e.g. waiting on an MCP elicitation) must not be resumed")
	}
}

func TestResumeIfReadyResumesOnce(t *testing.T) {
	store := newResumeReadyStore(core.RunStatusInterrupted)
	recorder := newResumeRecorder()
	release := make(chan struct{})
	resumer := NewRunResumeService(store).WithResume(func(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*runtime.Result, error) {
		<-release
		return recorder.resume(ctx, runID, targets, sink)
	})
	store.events[0].Payload = map[string]any{"interrupt": map[string]any{"contexts": []map[string]any{{"id": "i1", "is_root_cause": true}}}}
	for range 5 {
		if err := resumer.ResumeIfReady(context.Background(), "run_1"); err != nil {
			t.Fatalf("resume if ready: %v", err)
		}
	}
	close(release)
	recorder.waitTargets(t)
	time.Sleep(50 * time.Millisecond)
	if got := recorder.calls.Load(); got != 1 {
		t.Fatalf("resume ran %d times, want 1", got)
	}
}

func TestResumeFailureMarksRunFailed(t *testing.T) {
	store := newResumeReadyStore(core.RunStatusInterrupted)
	store.events[0].Payload = map[string]any{"interrupt": map[string]any{"contexts": []map[string]any{{"id": "i1", "is_root_cause": true}}}}
	recorder := newResumeRecorder()
	recorder.result = nil
	recorder.err = errors.New("checkpoint[run_1] not exist")
	if err := NewRunResumeService(store).WithResume(recorder.resume).ResumeIfReady(context.Background(), "run_1"); err != nil {
		t.Fatalf("resume if ready: %v", err)
	}
	recorder.waitTargets(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		store.mu.Lock()
		finished := append([]core.RunStatus(nil), store.finished...)
		last := store.events[len(store.events)-1]
		store.mu.Unlock()
		if len(finished) == 1 {
			if finished[0] != core.RunStatusFailed || last.Kind != "run.failed" {
				t.Fatalf("finished = %v, last event = %s", finished, last.Kind)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("resume failure was not recorded on the run")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
