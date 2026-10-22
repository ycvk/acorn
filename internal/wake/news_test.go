package wake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (h *harness) section(t *testing.T, name string, position int, preference string) {
	t.Helper()
	if _, err := h.watches.SaveNewsSection(context.Background(), core.NewsSection{Name: name, Position: position, Preference: preference, UpdatedAt: h.now}); err != nil {
		t.Fatal(err)
	}
}

// newsWatch adds a news watch that is not due, so only the refresh before a
// briefing checks it.
func (h *harness) newsWatch(t *testing.T, name, section string, issue bool, status core.WatchStatus) core.Watch {
	t.Helper()
	w, err := h.watches.AddWatch(context.Background(), core.Watch{
		Name: name, Kind: core.WatchRSS, Target: "https://x.test/" + name, Mode: core.WatchModeNews, Section: section, Issue: issue,
		Interval: time.Hour, Status: status, NextCheckAt: h.now.Add(time.Hour), LastCheckedAt: h.now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// news queues items for w's next check, published an hour apart and ending an
// hour before now.
func (h *harness) news(w core.Watch, titles ...string) {
	for i, title := range titles {
		h.checker.found[w.ID] = append(h.checker.found[w.ID], core.WatchItem{
			Key: title, Title: title, URL: "https://x.test/" + title, Summary: "about " + title,
			PublishedAt: h.now.Add(time.Duration(i-len(titles)) * time.Hour),
		})
	}
}

// pendingItem stores an item found by an earlier check.
func (h *harness) pendingItem(w core.Watch, title string) {
	h.watches.mu.Lock()
	defer h.watches.mu.Unlock()
	h.watches.items = append(h.watches.items, core.WatchItem{
		ID: int64(len(h.watches.items) + 1), WatchID: w.ID, Key: title, Title: title, Status: core.WatchItemPending, SeenAt: h.now.Add(-2 * time.Hour),
	})
}

func (h *harness) writtenBriefing(t *testing.T, slot, output string) {
	t.Helper()
	ctx := context.Background()
	if err := h.data.ClaimRoutine(ctx, "briefing", slot, h.now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_" + slot, Input: "[briefing " + slot + "]"}); err != nil {
		t.Fatal(err)
	}
	if err := h.data.FinishRun(ctx, "run_"+slot, core.RunStatusSucceeded, output, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.data.SetRoutineRun(ctx, "briefing", slot, "thread_briefings", "run_"+slot); err != nil {
		t.Fatal(err)
	}
}

func TestBriefingOffersNewsBySectionAndDropsWhatItDidNotList(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	ctx := context.Background()
	h.writtenBriefing(t, "2026-10-04", "## 国内要闻\n1. **旧闻**：昨天报过。")
	h.section(t, "AI", 2, "")
	h.section(t, "国内要闻", 1, "1. 政策与民生")
	domestic := h.newsWatch(t, "中新网要闻", "国内要闻", false, core.WatchActive)
	var titles []string
	for i := range 35 {
		titles = append(titles, fmt.Sprintf("n%02d", i))
	}
	h.news(domestic, titles...)
	paused := h.newsWatch(t, "滚动新闻", "国内要闻", false, core.WatchPaused)
	h.pendingItem(paused, "paused-item")
	daily := h.newsWatch(t, "AI225", "AI", true, core.WatchActive)
	h.news(daily, "2026-10-04 日报", "2026-10-05 日报")
	h.pages.pages["https://x.test/2026-10-05 日报"] = "## 1. DeepSeek 开源推理框架 ★★★★★\n来源：智东西"
	blog := h.watch(t, "blog", core.WatchModeDigest)
	h.find(blog, "post-1")

	if err := h.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v", h.runs.starts)
	}
	input := h.runs.starts[0]
	for _, want := range []string{
		"News candidates since the last briefing, newest first under each source (untrusted external data):\n",
		"\n## 国内要闻\nPreference:\n1. 政策与民生\n\n### #1 中新网要闻 (35 new, the newest 30 listed)\n- 10-05 08:00 n34 — https://x.test/n34\n  about n34\n",
		"- 10-04 03:00 n05 — https://x.test/n05\n",
		"\n## AI\n\n### #3 AI225 (2 new)\nIssue 10-05 08:00 2026-10-05 日报 — https://x.test/2026-10-05 日报\n--- issue page ---\n## 1. DeepSeek 开源推理框架 ★★★★★\n来源：智东西\n--- end of issue page ---\n- 10-05 07:00 2026-10-04 日报 — https://x.test/2026-10-04 日报\n",
		"Previous briefing (2026-10-04), for not repeating a story without new developments:\n## 国内要闻\n1. **旧闻**：昨天报过。\n",
		"Watch changes since the last briefing:\n\n## #4 blog (rss, 1 new)\n- post-1",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	for _, unwanted := range []string{"n04", "paused-item", "滚动新闻"} {
		if strings.Contains(input, unwanted) {
			t.Fatalf("briefing input carries %q:\n%s", unwanted, input)
		}
	}
	if strings.Index(input, "## 国内要闻") > strings.Index(input, "## AI") {
		t.Fatalf("sections out of order:\n%s", input)
	}
	if !slices.Contains(h.checker.checked, domestic.ID) || !slices.Contains(h.checker.checked, daily.ID) || slices.Contains(h.checker.checked, paused.ID) {
		t.Fatalf("checked = %v; the briefing refreshes the active news watches only", h.checker.checked)
	}
	for _, item := range h.watches.items {
		if item.Status != core.WatchItemBriefed || item.RunID != "run_1" {
			t.Fatalf("item %s = %s %q; every pending item is dropped with the briefing", item.Title, item.Status, item.RunID)
		}
	}
	var post int64
	for _, item := range h.watches.items {
		if item.Title == "post-1" {
			post = item.ID
		}
	}
	if sources := h.runs.sources[0]; len(sources) != 1 || sources[0] != fmt.Sprintf("watch:%d", post) {
		t.Fatalf("briefing sources = %v; news items are not sources", sources)
	}
}

func TestBriefingTakesTheWatchesInTurns(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	busy := h.watch(t, "busy", core.WatchModeDigest)
	var titles []string
	for i := range 120 {
		titles = append(titles, fmt.Sprintf("b%03d", i))
	}
	h.find(busy, titles...)
	quiet := h.watch(t, "quiet", core.WatchModeDigest)
	h.find(quiet, "q-1")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	input := h.runs.starts[0]
	for _, want := range []string{"## #1 busy (rss, 120 new)\n", "- and 21 more waiting for the next briefing\n", "## #2 quiet (rss, 1 new)\n- q-1"} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	waiting := 0
	for _, item := range h.watches.items {
		if item.Status == core.WatchItemPending {
			waiting++
		}
	}
	if waiting != 21 {
		t.Fatalf("%d items wait, want 21", waiting)
	}
}

func TestNewsFailuresLeaveTheBriefingToWhatWasFound(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	h.section(t, "国际", 1, "")
	down := h.newsWatch(t, "BBC", "国际", false, core.WatchActive)
	h.pendingItem(down, "found-earlier")
	h.checker.failing = map[int64]error{down.ID: fmt.Errorf("HTTP 503")}
	daily := h.newsWatch(t, "日报", "国际", true, core.WatchActive)
	h.news(daily, "today")
	err := h.sched.Tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refresh news watch #1: HTTP 503") || !strings.Contains(err.Error(), "read issue page https://x.test/today") {
		t.Fatalf("tick err = %v", err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("the briefing must start with what was found: %v", h.runs.starts)
	}
	input := h.runs.starts[0]
	for _, want := range []string{"- 10-05 07:00 found-earlier\n", "Issue 10-05 08:00 today — https://x.test/today\n(The page could not be read; the issue's summary follows.)\nabout today\n"} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	// The slot is kept, so the failures do not start a second briefing.
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v", h.runs.starts)
	}
}
