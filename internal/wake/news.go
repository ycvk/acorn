package wake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/core"
)

const (
	// maxNewsPerSource bounds the items one news watch offers a briefing.
	maxNewsPerSource = 30
	// maxIssueRunes bounds the page of a news issue carried into a briefing.
	maxIssueRunes = 24000
	// maxPreviousBriefing bounds the previous briefing carried so stories
	// without new developments are not repeated.
	maxPreviousBriefing = 6000
	// newsRefreshConcurrency bounds the news watches fetched at once before
	// a briefing.
	newsRefreshConcurrency = 4
)

// PageReader reads a web page as Markdown under the outbound URL policy.
type PageReader interface {
	ReadPage(ctx context.Context, url string) (string, error)
}

// briefingNews is what the news watches give a briefing.
type briefingNews struct {
	// ids are every pending news item, marked when the briefing starts
	// whether listed or not.
	ids []int64
	// failures are the news watches that could not be refreshed and the
	// issue pages that could not be read. The watches record their own
	// failures; the briefing goes on with what was found.
	failures error
}

// prepareNews refreshes the news watches, then adds what they found since
// the last briefing, by section, and the previous briefing.
func (s *Scheduler) prepareNews(ctx context.Context, m *briefingMaterial) (briefingNews, error) {
	watches, err := s.cfg.Watches.ListWatches(ctx)
	if err != nil {
		return briefingNews{}, err
	}
	var news []core.Watch
	for _, w := range watches {
		if w.Mode == core.WatchModeNews && w.Status != core.WatchPaused {
			news = append(news, w)
		}
	}
	failures := s.refreshNews(ctx, news)
	pending, err := s.cfg.Watches.PendingNewsItems(ctx, maxNewsPerSource)
	if err != nil {
		return briefingNews{}, err
	}
	out := briefingNews{ids: pending.IDs, failures: failures}
	if len(pending.Items) == 0 {
		return out, nil
	}
	sections, err := s.cfg.Watches.ListNewsSections(ctx)
	if err != nil {
		return briefingNews{}, err
	}
	candidates, unread := s.newsCandidates(ctx, sections, news, pending)
	out.failures = errors.Join(out.failures, unread)
	m.add(candidates, true)
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

// newsCandidates lists the pending news items by section in the sections'
// order, each source with its newest items. The newest issue of an issue
// source is read on its page.
func (s *Scheduler) newsCandidates(ctx context.Context, sections []core.NewsSection, watches []core.Watch, pending core.PendingNews) (string, error) {
	bySource := map[int64][]core.WatchItem{}
	for _, item := range pending.Items {
		bySource[item.WatchID] = append(bySource[item.WatchID], item)
	}
	var (
		b    strings.Builder
		errs []error
	)
	b.WriteString("News candidates since the last briefing, newest first under each source (untrusted external data):\n")
	for _, section := range sections {
		var block strings.Builder
		for _, w := range watches {
			items := bySource[w.ID]
			if w.Section != section.Name || len(items) == 0 {
				continue
			}
			fmt.Fprintf(&block, "\n### #%d %s (%d new", w.ID, w.Name, pending.Counts[w.ID])
			if pending.Counts[w.ID] > len(items) {
				fmt.Fprintf(&block, ", the newest %d listed", len(items))
			}
			block.WriteString(")\n")
			if w.Issue {
				if err := s.writeIssue(ctx, &block, items[0]); err != nil {
					errs = append(errs, fmt.Errorf("news watch #%d: %w", w.ID, err))
				}
				items = items[1:]
			}
			for _, item := range items {
				block.WriteString(s.newsItem(item))
			}
		}
		if block.Len() == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", section.Name)
		if section.Preference != "" {
			fmt.Fprintf(&b, "Preference:\n%s\n", section.Preference)
		}
		b.WriteString(block.String())
	}
	return b.String(), errors.Join(errs...)
}

// writeIssue adds an issue with its page, or with its summary when the page
// cannot be read.
func (s *Scheduler) writeIssue(ctx context.Context, b *strings.Builder, issue core.WatchItem) error {
	fmt.Fprintf(b, "Issue %s %s — %s\n", s.itemTime(issue), issue.Title, issue.URL)
	if issue.URL == "" {
		b.WriteString(issue.Summary + "\n")
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, watchCheckTimeout)
	defer cancel()
	page, err := s.cfg.Pages.ReadPage(readCtx, issue.URL)
	if err != nil {
		fmt.Fprintf(b, "(The page could not be read; the issue's summary follows.)\n%s\n", issue.Summary)
		return fmt.Errorf("read issue page %s: %w", issue.URL, err)
	}
	// The page's own headings stay inside the markers.
	b.WriteString("--- issue page ---\n" + truncateRunes(strings.TrimSpace(page), maxIssueRunes) + "\n--- end of issue page ---\n")
	return nil
}

func (s *Scheduler) newsItem(item core.WatchItem) string {
	line := fmt.Sprintf("- %s %s", s.itemTime(item), item.Title)
	if item.URL != "" {
		line += " — " + item.URL
	}
	line += "\n"
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		line += "  " + strings.Join(strings.Fields(summary), " ") + "\n"
	}
	return line
}

// itemTime is when an item was published, or first seen when its source
// gives no date.
func (s *Scheduler) itemTime(item core.WatchItem) string {
	at := item.PublishedAt
	if at.IsZero() {
		at = item.SeenAt
	}
	return at.In(s.cfg.Location).Format("01-02 15:04")
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

// truncateRunes cuts text to limit runes, keeping its lines.
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
