package wake

import (
	"context"
	"fmt"
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
// reactions and the night review counts them.
func (s *Scheduler) recordBriefingPicks(ctx context.Context, now time.Time) error {
	briefings, err := s.cfg.News.UnrecordedBriefings(ctx, now.Add(-core.NewsRetention))
	if err != nil || len(briefings) == 0 {
		return err
	}
	sections, err := s.cfg.Watches.ListNewsSections(ctx)
	if err != nil {
		return err
	}
	for _, briefing := range briefings {
		stories, err := s.cfg.News.BriefedNewsStories(ctx, briefing.RunID)
		if err != nil {
			return err
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
			return err
		}
	}
	return nil
}

// pickBriefingStories reads the stories a briefing picked from its reply:
// each numbered line under a news section's heading whose link is an item of
// a story the briefing offered, and the lines of today's focus naming them
// by number. Lines that match no offered story or no picked number are
// counted as unmatched.
func pickBriefingStories(body string, sections []string, stories []core.NewsStory) ([]core.BriefingStory, int) {
	byURL := map[string]*core.NewsStory{}
	for i := range stories {
		for _, item := range stories[i].Items {
			key := linkKey(item.URL)
			if _, taken := byURL[key]; key != "" && !taken {
				byURL[key] = &stories[i]
			}
		}
	}
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
		story, url := linkedStory(m[2], byURL)
		if err != nil || story == nil {
			unmatched++
			continue
		}
		if i, ok := position[story.ID]; ok {
			numbered[n] = i
			continue
		}
		title, summary := newsLineText(m[2])
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

// linkedStory is the story of the first link in text that is one of its
// items, with that link.
func linkedStory(text string, byURL map[string]*core.NewsStory) (*core.NewsStory, string) {
	for _, m := range markdownLink.FindAllStringSubmatch(text, -1) {
		if story, ok := byURL[linkKey(m[1])]; ok {
			return story, m[1]
		}
	}
	return nil, ""
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

func linkKey(url string) string {
	return strings.TrimSuffix(strings.TrimSpace(url), "/")
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

// picksHealth reports lines of the last briefing that matched no story it
// offered, which the owner cannot react to.
func (s *Scheduler) picksHealth(ctx context.Context) (string, error) {
	unmatched, err := s.cfg.News.LastBriefingUnmatched(ctx)
	if err != nil || unmatched == 0 {
		return "", err
	}
	return fmt.Sprintf("Briefing news: %d news or focus lines of the last briefing matched no story it offered, so the now page cannot list them for the owner's reactions; each news line must link an item of its story and each focus line start with 第 N 条.", unmatched), nil
}
