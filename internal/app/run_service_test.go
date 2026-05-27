package app

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/runtime"
)

func TestRunServiceInterruptRunDelegatesToSharedController(t *testing.T) {
	controller := runtime.NewRunController()
	service := NewRunService(nil, controller)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.Register("run_1", cancel)

	if err := service.InterruptRun(ctx, "run_1"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}

	select {
	case <-ctx.Done():
		// success: context was cancelled by the controller
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for interrupt to cancel context")
	}
}
