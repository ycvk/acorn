package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestRunControllerInterruptsRegisteredRun(t *testing.T) {
	controller := NewRunController()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.Register("run_active", cancel)
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
	_, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	ctx, cancelNew := context.WithCancel(context.Background())
	defer cancelNew()

	unregisterOld := controller.Register("run_1", cancelOld)
	controller.Register("run_1", cancelNew)
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
