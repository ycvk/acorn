package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// Each active execution follows the durable exclusion epoch. This also stops
// model calls owned by a different Acorn process sharing the database.
func watchRunMemory(ctx context.Context, store core.MemoryStore, runID string, epoch int64, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, err := store.MemoryEpoch(ctx)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("memory visibility epoch", "run_id", runID, "error", err)
					cancel(fmt.Errorf("memory visibility epoch: %w", err))
				}
				return
			}
			if current == epoch {
				continue
			}
			exclusion, err := store.MemoryExclusions(ctx)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("memory visibility exclusions", "run_id", runID, "error", err)
					cancel(fmt.Errorf("memory visibility exclusions: %w", err))
				}
				return
			}
			epoch = exclusion.Epoch
			if exclusion.RequestRunID != runID && slices.Contains(exclusion.RunIDs, runID) {
				cancel(core.ErrMemoryExcluded)
				return
			}
		}
	}
}
