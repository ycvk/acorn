package wake

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// focusHeading is the briefing section listing today's focus.
const focusHeading = "今日关注"

var (
	// newsLine is a numbered news line: `3. **标题**：一句话。[来源](链接)`.
	newsLine = regexp.MustCompile(`^\s*(\d+)[.、)]\s+(.*)$`)
	// focusLine names the news line it picks: `- 第 3 条：为什么重要`.
	focusLine    = regexp.MustCompile(`^\s*(?:[-*]|\d+[.、)])\s*第\s*(\d+)\s*条`)
	markdownLink = regexp.MustCompile(`\[[^\]]*\]\(<?([^)\s>]+)>?(?:\s+"[^"]*")?\)`)
	boldText     = regexp.MustCompile(`\*\*(.+?)\*\*`)
)

// recordBriefingPicks records what each briefing that succeeded since the
// last check picked, so the now page lists its stories for the owner's
// reactions and the night review counts them, then pushes its focus. The
// picks are recorded first, so a briefing is pushed at most once.
func (s *Scheduler) recordBriefingPicks(ctx context.Context, now time.Time) error {
	briefings, err := s.cfg.News.UnrecordedBriefings(ctx, now.Add(-core.NewsRetention))
	if err != nil || len(briefings) == 0 {
		return err
	}
	sections, err := s.cfg.Watches.ListNewsSections(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, briefing := range briefings {
		stories, err := s.cfg.News.BriefedNewsStories(ctx, briefing.RunID)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		names := make([]string, 0, len(sections)+len(stories))
		for _, section := range sections {
			names = append(names, section.Name)
		}
		for _, story := range stories {
			names = append(names, story.Section)
		}
		picked, unmatched := pickBriefingStories(briefing.Text, names, stories)
		if err := s.cfg.News.RecordBriefingPicks(ctx, core.BriefingPicks{RunID: briefing.RunID, BriefedAt: briefing.FinishedAt, Stories: picked, Unmatched: unmatched}); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if unmatched > 0 {
			slog.Warn("briefing lines matched no story it offered; the now page leaves them out", "run_id", briefing.RunID, "slot", briefing.Slot, "unmatched", unmatched, "picked", len(picked))
		}
		if len(stories) > 0 && len(picked) == 0 {
			slog.Error("briefing reply lists none of the news it was offered", "run_id", briefing.RunID, "slot", briefing.Slot, "offered", len(stories))
		}
		if err := s.pushBriefing(ctx, briefing); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// pickBriefingStories reads the stories a briefing picked from its reply:
// each numbered line under a news section's heading whose link is an item of
// a story the briefing offered, and the lines of today's focus naming them
// by number. Lines that match no offered story or no picked number are
// counted as unmatched.
func pickBriefingStories(body string, sections []string, stories []core.NewsStory) ([]core.BriefingStory, int) {
	links := indexStoryLinks(stories)
	var (
		picks     []core.BriefingStory
		unmatched int
		heading   string
		focus     []int
		numbered  = map[int]int{}
		position  = map[int64]int{}
	)
	for line := range strings.SplitSeq(body, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "## "); ok {
			heading = strings.TrimSpace(h)
			continue
		}
		if heading == focusHeading {
			if m := focusLine.FindStringSubmatch(line); m != nil {
				n, err := strconv.Atoi(m[1])
				if err != nil {
					unmatched++
					continue
				}
				focus = append(focus, n)
			}
			continue
		}
		if !slices.Contains(sections, heading) {
			continue
		}
		m := newsLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		title, summary := newsLineText(m[2])
		story, url := links.story(m[2], title, position)
		if err != nil || story == nil {
			unmatched++
			continue
		}
		if i, ok := position[story.ID]; ok {
			numbered[n] = i
			continue
		}
		position[story.ID], numbered[n] = len(picks), len(picks)
		picks = append(picks, core.BriefingStory{StoryID: story.ID, Section: heading, Title: title, Summary: summary, URL: url, Sources: storySources(*story)})
	}
	for _, n := range focus {
		i, ok := numbered[n]
		if !ok {
			unmatched++
			continue
		}
		picks[i].Focus = true
	}
	return picks, unmatched
}

// storyLinks finds offered stories by the address of one of their items.
// A briefing drops a link's fragment or a feed's tracking query (BBC's
// ?at_medium=RSS), and the entries of an issue share its page's address.
type storyLinks struct {
	// full keys addresses without their fragment, bare without their query
	// too; each lists the stories with such an item in offered order.
	full, bare map[string][]*core.NewsStory
}

func indexStoryLinks(stories []core.NewsStory) storyLinks {
	links := storyLinks{full: map[string][]*core.NewsStory{}, bare: map[string][]*core.NewsStory{}}
	add := func(index map[string][]*core.NewsStory, key string, story *core.NewsStory) {
		if key != "" && !slices.Contains(index[key], story) {
			index[key] = append(index[key], story)
		}
	}
	for i := range stories {
		for _, item := range stories[i].Items {
			full := linkKey(item.URL)
			add(links.full, full, &stories[i])
			bare, _, _ := strings.Cut(full, "?")
			add(links.bare, strings.TrimSuffix(bare, "/"), &stories[i])
		}
	}
	return links
}

// story is the story the first matching link of a news line points to,
// with that link. When the address is shared, the line's title decides,
// and a story not yet picked comes before one that was.
func (l storyLinks) story(text, title string, picked map[int64]int) (*core.NewsStory, string) {
	for _, m := range markdownLink.FindAllStringSubmatch(text, -1) {
		key := linkKey(m[1])
		candidates := l.full[key]
		if len(candidates) == 0 && !strings.Contains(key, "?") {
			candidates = l.bare[key]
		}
		if len(candidates) == 0 {
			continue
		}
		best, bestScore := candidates[0], -1
		for _, story := range candidates {
			score := titleOverlap(title, *story) * 2
			if _, ok := picked[story.ID]; !ok {
				score++
			}
			if score > bestScore {
				best, bestScore = story, score
			}
		}
		return best, m[1]
	}
	return nil, ""
}

// titleOverlap counts the character pairs of title found in the story's
// title or in one of its items' titles, whichever shares the most.
func titleOverlap(title string, story core.NewsStory) int {
	pairs := func(text string) []string {
		runes := []rune(strings.ToLower(text))
		var out []string
		for i := 0; i+1 < len(runes); i++ {
			out = append(out, string(runes[i:i+2]))
		}
		return out
	}
	mine := pairs(title)
	best := 0
	for _, other := range append([]string{story.Title}, itemTitles(story)...) {
		theirs := pairs(other)
		n := 0
		for _, p := range mine {
			if slices.Contains(theirs, p) {
				n++
			}
		}
		best = max(best, n)
	}
	return best
}

func itemTitles(story core.NewsStory) []string {
	titles := make([]string, 0, len(story.Items))
	for _, item := range story.Items {
		titles = append(titles, item.Title)
	}
	return titles
}

// newsLineText splits a news line into its bold title and the sentence
// after it, without links.
func newsLineText(text string) (string, string) {
	text = strings.TrimSpace(markdownLink.ReplaceAllString(text, ""))
	m := boldText.FindStringSubmatchIndex(text)
	if m == nil {
		return text, ""
	}
	title := strings.TrimSpace(text[m[2]:m[3]])
	rest := strings.TrimSpace(text[:m[0]] + text[m[1]:])
	return title, strings.TrimSpace(strings.TrimLeft(rest, "：:—-– "))
}

// linkKey is an address without its fragment and trailing slash.
func linkKey(url string) string {
	key, _, _ := strings.Cut(strings.TrimSpace(url), "#")
	return strings.TrimSuffix(key, "/")
}

// storySources are the news sources of a story's items, in id order.
func storySources(story core.NewsStory) []int64 {
	var ids []int64
	for _, item := range story.Items {
		if !slices.Contains(ids, item.WatchID) {
			ids = append(ids, item.WatchID)
		}
	}
	slices.Sort(ids)
	return ids
}
