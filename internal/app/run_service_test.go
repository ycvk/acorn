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
	controller.Register("run_active", cancel)

	if err := service.InterruptRun(context.Background(), "run_active"); err != nil {
		t.Fatalf("InterruptRun: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for run cancellation")
	}
}
