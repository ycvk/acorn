package wake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// EventBriefingFired is recorded on the run of a morning briefing.
const EventBriefingFired = "briefing.fired"

// maxBriefingItems bounds the items one briefing input carries.
const maxBriefingItems = 100

// brief starts the day's morning briefing once the owner-local time has passed
// Briefing.At. It runs once per day across processes, also when serve starts
// later in the day; days serve did not run are not made up. Briefings do not
// count toward the daily wake limit.
func (s *Scheduler) brief(ctx context.Context, now time.Time) error {
	if !s.cfg.Briefing.Enabled {
		return nil
	}
	local := now.In(s.cfg.Location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.cfg.Location)
	if local.Before(midnight.Add(s.cfg.Briefing.At)) {
		return nil
	}
	day := local.Format("2006-01-02")
	if err := s.cfg.Watches.ClaimBriefing(ctx, day, now); err != nil {
		if errors.Is(err, core.ErrBriefingTaken) {
			return nil
		}
		return err
	}
	runID, err := s.startBriefing(ctx, day)
	if err != nil {
		if releaseErr := s.cfg.Watches.ReleaseBriefing(ctx, day); releaseErr != nil {
			return errors.Join(err, fmt.Errorf("release briefing claim: %w", releaseErr))
		}
		return err
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, EventBriefingFired, map[string]any{"day": day}); err != nil {
		return fmt.Errorf("record briefing run %s: %w", runID, err)
	}
	return nil
}

func (s *Scheduler) startBriefing(ctx context.Context, day string) (string, error) {
	items, err := s.cfg.Watches.ListWatchItems(ctx, core.WatchItemPending, maxBriefingItems)
	if err != nil {
		return "", err
	}
	watches, err := s.cfg.Watches.ListWatches(ctx)
	if err != nil {
		return "", err
	}
	thread, err := s.cfg.Watches.LatestBriefingThread(ctx)
	if err != nil {
		return "", err
	}
	thread, runID, err := s.cfg.Runs.StartBriefingRun(ctx, thread, "morning briefing "+day, briefingInput(day, watches, items))
	if err != nil {
		return "", fmt.Errorf("start briefing run: %w", err)
	}
	if err := s.cfg.Watches.SetBriefingRun(ctx, day, thread, runID); err != nil {
		return "", err
	}
	if err := s.cfg.Watches.MarkWatchItems(ctx, itemIDs(items), core.WatchItemBriefed, runID); err != nil {
		return "", err
	}
	return runID, nil
}

// briefingInput lists pending items grouped by watch, then the failing watches.
func briefingInput(day string, watches []core.Watch, items []core.WatchItem) string {
	byWatch := map[int64][]core.WatchItem{}
	for _, item := range items {
		byWatch[item.WatchID] = append(byWatch[item.WatchID], item)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[briefing %s] morning briefing\n", day)
	if len(items) == 0 {
		b.WriteString("No watch changes since the last briefing.\n")
	} else {
		b.WriteString("Watch changes since the last briefing:\n")
		for _, w := range watches {
			if found := byWatch[w.ID]; len(found) > 0 {
				fmt.Fprintf(&b, "\n## #%d %s (%s, %d new)\n%s", w.ID, w.Name, w.Kind, len(found), formatItems(found))
			}
		}
	}
	var failing []string
	for _, w := range watches {
		if w.Status == core.WatchFailing {
			failing = append(failing, fmt.Sprintf("- #%d %s: %s (%d failures in a row)", w.ID, w.Name, w.LastError, w.Failures))
		}
	}
	if len(failing) > 0 {
		b.WriteString("\nFailing watches:\n" + strings.Join(failing, "\n") + "\n")
	}
	return b.String()
}
