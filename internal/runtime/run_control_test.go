package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestRunControllerInterruptsRegisteredRun(t *testing.T) {
	controller := NewRunController()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	if _, err := controller.Register("run_active", cancel); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := controller.Interrupt("run_active"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for run cancellation")
	}
}

func TestRunControllerReturnsErrRunNotActive(t *testing.T) {
	controller := NewRunController()
	err := controller.Interrupt("run_missing")
	if !errors.Is(err, core.ErrRunNotActive) {
		t.Fatalf("Interrupt error = %v, want ErrRunNotActive", err)
	}
}

func TestRunControllerStaleUnregisterKeepsNewerRegistration(t *testing.T) {
	controller := NewRunController()
	_, cancelOld := context.WithCancelCause(context.Background())
	defer cancelOld(nil)
	ctx, cancelNew := context.WithCancelCause(context.Background())
	defer cancelNew(nil)

	unregisterOld, err := controller.Register("run_1", cancelOld)
	if err != nil {
		t.Fatalf("Register old: %v", err)
	}
	if _, err := controller.Register("run_1", cancelNew); err != nil {
		t.Fatalf("Register new: %v", err)
	}
	unregisterOld()

	if err := controller.Interrupt("run_1"); err != nil {
		t.Fatalf("Interrupt after stale unregister: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("newer registration was not cancelled")
	}
}

// TestRunControllerStopCancelsRunsAndRefusesNewOnes covers Acorn stopping:
// active runs are cancelled with core.ErrShuttingDown, Stop waits until they
// finish, and no run starts afterwards.
func TestRunControllerStopCancelsRunsAndRefusesNewOnes(t *testing.T) {
	controller := NewRunController()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	unregister, err := controller.Register("run_active", cancel)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	stopped := make(chan error, 1)
	go func() { stopped <- controller.Stop(context.Background()) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the active run")
	}
	if cause := context.Cause(ctx); !errors.Is(cause, core.ErrShuttingDown) {
		t.Fatalf("cancel cause = %v, want ErrShuttingDown", cause)
	}
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned %v before the run finished", err)
	case <-time.After(50 * time.Millisecond):
	}
	unregister()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the run finished")
	}

	if _, err := controller.Register("run_late", func(error) {}); !errors.Is(err, core.ErrShuttingDown) {
		t.Fatalf("Register after Stop = %v, want ErrShuttingDown", err)
	}
}

// TestRunControllerStopIsBounded covers a run that does not finish after its
// cancellation: Stop returns when ctx ends and names the run.
func TestRunControllerStopIsBounded(t *testing.T) {
	controller := NewRunController()
	if _, err := controller.Register("run_stuck", func(error) {}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := controller.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "run_stuck") {
		t.Fatalf("Stop = %v, want a deadline error naming run_stuck", err)
	}
}
