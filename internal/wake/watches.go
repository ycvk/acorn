package wake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	// watchLease keeps a claimed watch from being checked again while its
	// check runs, and lets a crashed check be retried.
	watchLease = 10 * time.Minute
	// watchCheckTimeout bounds one watch's fetch.
	watchCheckTimeout = 60 * time.Second
	// maxListedItems is how many items a wake input lists in full.
	maxListedItems = 20
)

// checkWatches fetches the due watches. New items of digest watches wait for
// the briefing; new items of immediate watches wake the agent in the watch's
// conversation while the daily limit allows, and otherwise wait too.
func (s *Scheduler) checkWatches(ctx context.Context, now time.Time) error {
	due, err := s.cfg.Watches.ListDueWatches(ctx, now, s.cfg.MaxChecksPerTick)
	if err != nil {
		return fmt.Errorf("list due watches: %w", err)
	}
	var errs []error
	for _, w := range due {
		if err := s.cfg.Watches.ClaimDueWatch(ctx, w.ID, now, watchLease); err != nil {
			if errors.Is(err, core.ErrWatchNotDue) {
				continue // another scheduler took it
			}
			errs = append(errs, err)
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, watchCheckTimeout)
		result, err := s.cfg.Checker.Check(checkCtx, w)
		cancel()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(result.New) == 0 || w.Mode != core.WatchModeImmediate {
			continue
		}
		if err := s.wakeForWatch(ctx, result.Watch, result.New, now); err != nil {
			errs = append(errs, fmt.Errorf("watch #%d: %w", w.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) wakeForWatch(ctx context.Context, w core.Watch, items []core.WatchItem, now time.Time) error {
	allowed, err := s.withinBudget(ctx, fmt.Sprintf("watch #%d", w.ID), now)
	if err != nil || !allowed {
		return err // the items stay pending for the briefing
	}
	wake := fmt.Sprintf("watch #%d %s: %d new", w.ID, w.Name, len(items))
	input := fmt.Sprintf("[watch #%d %s] %d new\n%s", w.ID, w.Name, len(items), formatItems(items))
	runID, err := s.cfg.Runs.StartWakeRun(ctx, w.SessionID, wake, input)
	if err != nil {
		return fmt.Errorf("start watch run (items wait for the briefing): %w", err)
	}
	if err := s.cfg.Watches.MarkWatchItems(ctx, itemIDs(items), core.WatchItemWoken, runID); err != nil {
		return err
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, core.EventWakeFired, map[string]any{"watch_id": w.ID}); err != nil {
		return fmt.Errorf("record wake of run %s: %w", runID, err)
	}
	return nil
}

// formatItems lists items for a wake input: title and link, then the summary
// indented. Past maxListedItems only the count is given.
func formatItems(items []core.WatchItem) string {
	var b strings.Builder
	for i, item := range items {
		if i == maxListedItems {
			fmt.Fprintf(&b, "- and %d more\n", len(items)-maxListedItems)
			break
		}
		b.WriteString("- " + item.Title)
		if item.URL != "" {
			b.WriteString(" — " + item.URL)
		}
		b.WriteString("\n")
		for _, line := range strings.Split(item.Summary, "\n") {
			if strings.TrimSpace(line) != "" {
				b.WriteString("  " + line + "\n")
			}
		}
	}
	return b.String()
}

func itemIDs(items []core.WatchItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
