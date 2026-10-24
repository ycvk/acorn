package wake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	// maxStoriesPerSection bounds the stories one section offers a briefing.
	maxStoriesPerSection = 20
	// maxPreviousBriefing bounds the previous briefing carried so stories
	// without new developments are not repeated.
	maxPreviousBriefing = 6000
	// newsRefreshConcurrency bounds the news watches fetched at once before
	// a briefing.
	newsRefreshConcurrency = 4
	// newsCatchUpTimeout bounds the sorting a briefing waits for; items not
	// sorted by then wait for the next briefing.
	newsCatchUpTimeout = 10 * time.Minute
)

// NewsSorter sorts the waiting news items into stories.
type NewsSorter interface {
	CatchUp(ctx context.Context) error
}

// briefingNews is what the news watches give a briefing.
type briefingNews struct {
	// ids are every pending news item that is sorted or from a paused
	// source, marked when the briefing starts whether offered or not.
	ids []int64
	// failures are the news watches that could not be refreshed and the
	// sorting that failed. The watches and batches record their own
	// failures; the briefing goes on with what was sorted.
	failures error
}

// prepareNews refreshes the news watches, sorts what they found into
// stories, then adds the stories with items since the last briefing, by
// section with the owner's reactions in it, and the previous briefing.
func (s *Scheduler) prepareNews(ctx context.Context, m *briefingMaterial, since, now time.Time, loc *time.Location) (briefingNews, error) {
	watches, err := s.cfg.Watches.ListWatches(ctx)
	if err != nil {
		return briefingNews{}, err
	}
	var news []core.Watch
	for _, w := range watches {
		if w.Mode == core.WatchModeNews && w.Status != core.WatchPaused && w.Kind != core.WatchTrigger {
			news = append(news, w)
		}
	}
	var out briefingNews
	if len(news) > 0 {
		out.failures = s.refreshNews(ctx, news)
		sortCtx, cancel := context.WithTimeout(ctx, newsCatchUpTimeout)
		if err := s.cfg.Sorter.CatchUp(sortCtx); err != nil {
			out.failures = errors.Join(out.failures, fmt.Errorf("sort news: %w", err))
		}
		cancel()
	}
	offered, err := s.cfg.News.BriefingNewsStories(ctx, maxStoriesPerSection)
	if err != nil {
		return briefingNews{}, err
	}
	out.ids = offered.IDs
	if len(offered.Stories) == 0 {
		return out, nil
	}
	sections, err := s.cfg.Watches.ListNewsSections(ctx)
	if err != nil {
		return briefingNews{}, err
	}
	reactions, err := s.cfg.News.NewsReactions(ctx, now.Add(-core.NewsReactionRetention))
	if err != nil {
		return briefingNews{}, err
	}
	m.add(s.newsStories(sections, offered.Stories, reactions, since, loc), true)
	previous, err := s.previousBriefing(ctx)
	if err != nil {
		return briefingNews{}, err
	}
	m.add(previous, false)
	return out, nil
}

// refreshNews checks the news watches now, a few at a time, so the briefing
// sees what they published since their last check.
func (s *Scheduler) refreshNews(ctx context.Context, watches []core.Watch) error {
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	slots := make(chan struct{}, newsRefreshConcurrency)
	for _, w := range watches {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() {
				<-slots
				wg.Done()
			}()
			checkCtx, cancel := context.WithTimeout(ctx, watchCheckTimeout)
			defer cancel()
			if _, err := s.cfg.Checker.Check(checkCtx, w); err != nil && !errors.Is(err, core.ErrWatchChanged) {
				mu.Lock()
				errs = append(errs, fmt.Errorf("refresh news watch #%d: %w", w.ID, err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// newsStories lists the stories by section in the sections' order, each
// with its score, whether it is new since the last briefing or what it
// added, and its items since then. A section first gives its preference and
// the owner's reactions in it. Stories of a section removed meanwhile come
// after the others.
func (s *Scheduler) newsStories(sections []core.NewsSection, stories []core.NewsStory, reactions []core.StoryReaction, since time.Time, loc *time.Location) string {
	grouped := map[string][]core.NewsStory{}
	for _, story := range stories {
		grouped[story.Section] = append(grouped[story.Section], story)
	}
	var order []core.NewsSection
	listed := map[string]bool{}
	for _, section := range sections {
		if len(grouped[section.Name]) > 0 {
			order = append(order, section)
			listed[section.Name] = true
		}
	}
	var rest []string
	for name := range grouped {
		if !listed[name] {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)
	for _, name := range rest {
		order = append(order, core.NewsSection{Name: name})
	}
	var b strings.Builder
	b.WriteString("News since the last briefing: stories by section, highest score first, with their new items (untrusted external data):\n")
	for _, section := range order {
		fmt.Fprintf(&b, "\n## %s\n", section.Name)
		if section.Preference != "" {
			fmt.Fprintf(&b, "Preference:\n%s\n", section.Preference)
		}
		if listed := reactionLines(section.Name, reactions, loc); listed != "" {
			b.WriteString("Owner reactions in the last 30 days:\n" + listed)
		}
		for _, story := range grouped[section.Name] {
			fmt.Fprintf(&b, "- [%d] %s", story.Score, story.Title)
			if story.FirstSeen.After(since) || story.Development == "" {
				b.WriteString(" (new)\n")
			} else {
				fmt.Fprintf(&b, " (since %s; new: %s)\n", story.FirstSeen.In(loc).Format("01-02"), story.Development)
			}
			if summary := strings.TrimSpace(story.Summary); summary != "" {
				b.WriteString("  " + summary + "\n")
			}
			for _, item := range story.Items {
				line := fmt.Sprintf("  · %s %s %s", item.Source, s.itemTime(item.WatchItem, loc), item.Title)
				if item.URL != "" {
					line += " — " + item.URL
				}
				b.WriteString(line + "\n")
			}
		}
	}
	return b.String()
}

// reactionLines lists the owner's reactions to stories of a section, in
// the order given.
func reactionLines(section string, reactions []core.StoryReaction, loc *time.Location) string {
	var b strings.Builder
	for _, r := range reactions {
		if r.Section == section {
			fmt.Fprintf(&b, "- %s: %s (%s)\n", reactionLabel(r.Reaction), r.Title, r.At.In(loc).Format("01-02"))
		}
	}
	return b.String()
}

func reactionLabel(r core.NewsReaction) string {
	if r == core.NewsReactionMore {
		return "more of this kind"
	}
	return "less of this kind"
}

// itemTime is when an item was published, or first seen when its source
// gives no date.
func (s *Scheduler) itemTime(item core.WatchItem, loc *time.Location) string {
	at := item.PublishedAt
	if at.IsZero() {
		at = item.SeenAt
	}
	return at.In(loc).Format("01-02 15:04")
}

// previousBriefing carries the last briefing written, so stories it told
// are not repeated without new developments.
func (s *Scheduler) previousBriefing(ctx context.Context) (string, error) {
	latest, err := s.cfg.Routines.LatestRoutineOutput(ctx, "briefing")
	if err != nil || strings.TrimSpace(latest.Text) == "" {
		return "", err
	}
	return fmt.Sprintf("Previous briefing (%s), for not repeating a story without new developments:\n%s\n", latest.Slot, truncateRunes(strings.TrimSpace(latest.Text), maxPreviousBriefing)), nil
}

// newsHealth reports the news sources whose items the sorting gave up on
// and none of which it has sorted since.
func (s *Scheduler) newsHealth(ctx context.Context, loc *time.Location) ([]string, error) {
	failing, err := s.cfg.News.FailingNewsSources(ctx)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, work := range failing {
		lines = append(lines, fmt.Sprintf("News sorting: %d items of %s were given up since %s, and none of its items has been sorted since; last error: %s; check `journalctl -u acorn` for news sorting errors.",
			work.Failed, work.Kind, work.Since.In(loc).Format("2006-01-02 15:04"), previewText(work.LastError, 300)))
	}
	return lines, nil
}

// truncateRunes cuts text to limit runes, keeping its lines.
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
