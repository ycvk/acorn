package wire

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
)

func memoryForgetBarrier(controller *runtime.RunController, store core.SessionStore) func(context.Context, core.MemoryExclusion, string) error {
	return func(ctx context.Context, exclusion core.MemoryExclusion, caller string) error {
		if err := controller.ForgetBarrier(ctx, exclusion, caller); err != nil {
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		// A foreign process observes the committed epoch and cancels its execution;
		// the terminal run state confirms that it has stopped producing output.
		for {
			waiting := false
			for _, id := range exclusion.RunIDs {
				if id == caller {
					continue
				}
				run, err := store.LoadRun(waitCtx, id)
				if errors.Is(err, core.ErrRunNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				if run.Status == core.RunStatusRunning {
					waiting = true
				}
			}
			if !waiting {
				return nil
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-waitCtx.Done():
				timer.Stop()
				return fmt.Errorf("memory exclusions are committed; affected executions have not confirmed termination: %w", waitCtx.Err())
			case <-timer.C:
			}
		}
	}
}
