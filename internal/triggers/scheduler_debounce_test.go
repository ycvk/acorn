package triggers

import (
	"context"
	"testing"
	"time"
)

// TestSchedulerDebounceCoalescesRapidFires verifies that multiple fires of the
// same trigger within the debounce window are coalesced into a single run.
// Without debounce, a webhook spamming 100 requests would create 100 runs.
func TestSchedulerDebounceCoalescesRapidFires(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc, WithDebounce(50*time.Millisecond))

	sched.Register(&stubTrigger{id: "wh1"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	// Fire 5 times rapidly within the debounce window.
	for i := 0; i < 5; i++ {
		sched.Fire(ctx, "wh1", "event")
	}

	// Wait for debounce + processing.
	time.Sleep(200 * time.Millisecond)

	// Should be coalesced to 1 run.
	if got := rc.calls.Load(); got != 1 {
		t.Fatalf("CreateRun calls = %d, want 1 (debounced)", got)
	}
}

// TestSchedulerDebounceDifferentTriggersAreIndependent verifies that debounce
// is per-trigger: rapid fires of different triggers each get their own run.
func TestSchedulerDebounceDifferentTriggersAreIndependent(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc, WithDebounce(50*time.Millisecond))

	sched.Register(&stubTrigger{id: "wh1"})
	sched.Register(&stubTrigger{id: "wh2"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	sched.Fire(ctx, "wh1", "event-a")
	sched.Fire(ctx, "wh2", "event-b")

	time.Sleep(200 * time.Millisecond)

	if got := rc.calls.Load(); got != 2 {
		t.Fatalf("CreateRun calls = %d, want 2 (one per trigger)", got)
	}
}

// TestSchedulerDebounceLastInputWins verifies that when multiple fires are
// coalesced, the last input is the one passed to CreateRun (most recent state).
func TestSchedulerDebounceLastInputWins(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc, WithDebounce(50*time.Millisecond))

	sched.Register(&stubTrigger{id: "wh1"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	sched.Fire(ctx, "wh1", "first")
	sched.Fire(ctx, "wh1", "second")
	sched.Fire(ctx, "wh1", "third")

	time.Sleep(200 * time.Millisecond)

	if got := rc.calls.Load(); got != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", got)
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if len(rc.inputs) != 1 || rc.inputs[0] != "third" {
		t.Fatalf("inputs = %v, want [third]", rc.inputs)
	}
}

// TestSchedulerNoDebouncePassesAllFires verifies that without debounce (the
// default), every fire creates a run. This protects backward compatibility.
func TestSchedulerNoDebouncePassesAllFires(t *testing.T) {
	rc := &stubRunCreator{}
	sched := NewScheduler(rc) // no debounce option

	sched.Register(&stubTrigger{id: "wh1"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	sched.Fire(ctx, "wh1", "a")
	sched.Fire(ctx, "wh1", "b")

	time.Sleep(200 * time.Millisecond)

	if got := rc.calls.Load(); got != 2 {
		t.Fatalf("CreateRun calls = %d, want 2 (no debounce)", got)
	}
}
