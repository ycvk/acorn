package cli

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestResumeReadyRunsLoopSweepsAtStartupAndOnTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		resumeReadyRunsLoop(ctx, func(context.Context) error {
			calls.Add(1)
			return errors.New("store busy")
		}, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("sweeps = %d, want at least 3 (startup plus ticks, errors do not stop the loop)", calls.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop after context cancel")
	}
}
