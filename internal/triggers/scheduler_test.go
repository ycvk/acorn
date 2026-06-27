package triggers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubRunCreator records calls to CreateRun. It lets tests verify that a
type stubRunCreator struct {
	mu       sync.Mutex
	calls    atomic.Int32
	inputs   []string
	triggers []string
}

func (s *stubRunCreator) CreateRun(_ context.Context, triggerID, input string) error {
	s.calls.Add(1)
	s.mu.Lock()
	s.triggers = append(s.triggers, triggerID)
	s.inputs = append(s.inputs, input)
	s.mu.Unlock()
	return nil
}

// stubTrigger is a test Trigger that calls onFire once when started.
type stubTrigger struct {
	id      string
	started bool
	stopped bool
	onFire  func()
}

func (s *stubTrigger) ID() string { return s.id }

func (s *stubTrigger) Start(_ context.Context, _ FireFunc) error {
	s.started = true
	if s.onFire != nil {
		s.onFire()
	}
	return nil
}

func (s *stubTrigger) Stop() {
	s.stopped = true
}

func TestSchedulerStartRunsAllTriggers(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc)

	t1 := &stubTrigger{id: "wh1"}
	t2 := &stubTrigger{id: "wh2"}
	sched.Register(t1)
	sched.Register(t2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !t1.started || !t2.started {
		t.Fatalf("triggers not started: t1=%v t2=%v", t1.started, t2.started)
	}

	cancel()
	sched.Stop()

	if !t1.stopped || !t2.stopped {
		t.Fatalf("triggers not stopped: t1=%v t2=%v", t1.stopped, t2.stopped)
	}
}

func TestSchedulerFireCreatesRun(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc)

	fired := make(chan struct{})
	t1 := &stubTrigger{id: "wh1", onFire: func() {
		sched.Fire(context.Background(), "wh1", "webhook event: deploy failed")
		close(fired)
	}}
	sched.Register(t1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	// Wait for the trigger to fire.
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("trigger did not fire within 2s")
	}

	// Give the scheduler a moment to process the fire.
	deadline := time.After(2 * time.Second)
	for rc.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatalf("CreateRun not called, calls=%d", rc.calls.Load())
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	if rc.calls.Load() != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", rc.calls.Load())
	}
	rc.mu.Lock()
	triggers := append([]string(nil), rc.triggers...)
	inputs := append([]string(nil), rc.inputs...)
	rc.mu.Unlock()
	if len(triggers) != 1 || triggers[0] != "wh1" {
		t.Fatalf("trigger IDs = %v, want [wh1]", triggers)
	}
	if len(inputs) != 1 || inputs[0] != "webhook event: deploy failed" {
		t.Fatalf("inputs = %v, want [webhook event: deploy failed]", inputs)
	}

	cancel()
	sched.Stop()
}

func TestSchedulerStartEmptyIsNoop(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start with no triggers: %v", err)
	}
	sched.Stop()
}

func TestSchedulerFireUnknownTriggerIDIsIgnored(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc)

	// Fire with an ID that no trigger registered — should be silently
	// ignored, not panic.
	sched.Fire(context.Background(), "nonexistent", "hello")

	if rc.calls.Load() != 0 {
		t.Fatalf("CreateRun should not be called for unknown trigger, got %d", rc.calls.Load())
	}
}

func TestSchedulerHandleWebhookUnknownReturnsNotFoundError(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc)

	req := httptest.NewRequest(http.MethodPost, "/v1/triggers/unknown", strings.NewReader("{}"))
	err := sched.HandleWebhook(context.Background(), "unknown", req)
	if err == nil {
		t.Fatal("expected error for unknown trigger, got nil")
	}
	var nfe *TriggerNotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("error = %v, want *TriggerNotFoundError", err)
	}
	if nfe.ID != "unknown" {
		t.Fatalf("ID = %q, want 'unknown'", nfe.ID)
	}
}
