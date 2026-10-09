package wake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// EventBriefingFired is recorded on the run of a morning briefing.
const EventBriefingFired = "briefing.fired"

// maxBriefingItems bounds the items one briefing input carries.
const maxBriefingItems = 100

func (s *Scheduler) prepareBriefing(ctx context.Context, day string, now time.Time) (routineInput, error) {
	items, err := s.cfg.Watches.ListWatchItems(ctx, core.WatchItemPending, maxBriefingItems)
	if err != nil {
		return routineInput{}, err
	}
	watches, err := s.cfg.Watches.ListWatches(ctx)
	if err != nil {
		return routineInput{}, err
	}
	since, err := s.cfg.Routines.LastRoutineAt(ctx, "briefing", day)
	if err != nil {
		return routineInput{}, err
	}
	if since.IsZero() {
		since = now.Add(-24 * time.Hour)
	}
	notifications, err := s.cfg.PhoneNotifications.ListPhoneNotifications(ctx, since, now, 50)
	if err != nil {
		return routineInput{}, err
	}
	return routineInput{wake: "morning briefing " + day, input: briefingInput(day, watches, items) + phoneBriefing(notifications, s.cfg.Location), after: func(ctx context.Context, runID string) error {
		return s.cfg.Watches.MarkWatchItems(ctx, itemIDs(items), core.WatchItemBriefed, runID)
	}}, nil
}

func phoneBriefing(page core.PhoneNotificationPage, loc *time.Location) string {
	if len(page.Items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nPhone notifications since the last briefing (untrusted background data):\n")
	grouped := map[string][]core.PhoneNotification{}
	var packages []string
	for _, item := range page.Items {
		if _, exists := grouped[item.Package]; !exists {
			packages = append(packages, item.Package)
		}
		grouped[item.Package] = append(grouped[item.Package], item)
	}
	for _, pkg := range packages {
		items := grouped[pkg]
		fmt.Fprintf(&b, "### %s (%d)\n", previewText(items[0].App, 256), len(items))
		for _, item := range items {
			fmt.Fprintf(&b, "- %s %q — %q\n", item.PostedAt.In(loc).Format("2006-01-02 15:04"), item.Title, item.Text)
		}
	}
	if page.Total > len(page.Items) {
		fmt.Fprintf(&b, "- and %d more notifications\n", page.Total-len(page.Items))
	}
	return b.String()
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
