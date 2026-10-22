package wake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// fakeNews offers fixed stories and records what the scheduler asks of it.
type fakeNews struct {
	core.NewsStore
	mu      sync.Mutex
	offered core.BriefingNews
	failed  int
	pruned  []time.Time
	stats   []core.NewsSourceStats
	since   []time.Time
}

func (f *fakeNews) NewsSourceStats(_ context.Context, since time.Time) ([]core.NewsSourceStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	return f.stats, nil
}

func (f *fakeNews) BriefingNewsStories(context.Context, int) (core.BriefingNews, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.offered, nil
}

func (f *fakeNews) FailedNewsItems(context.Context, time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed, nil
}

func (f *fakeNews) PruneNews(_ context.Context, before time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruned = append(f.pruned, before)
	return 0, nil
}

// fakeSorter records when it sorted, after which watches were checked.
type fakeSorter struct {
	mu           sync.Mutex
	checker      *fakeChecker
	checkedFirst [][]int64
	err          error
}

func (s *fakeSorter) CatchUp(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checker.mu.Lock()
	s.checkedFirst = append(s.checkedFirst, append([]int64(nil), s.checker.checked...))
	s.checker.mu.Unlock()
	return s.err
}

func (h *harness) section(t *testing.T, name string, position int, preference string) {
	t.Helper()
	if _, err := h.watches.SaveNewsSection(context.Background(), core.NewsSection{Name: name, Position: position, Preference: preference, UpdatedAt: h.now}); err != nil {
		t.Fatal(err)
	}
}

// newsWatch adds a news watch that is not due, so only the refresh before a
// briefing checks it.
func (h *harness) newsWatch(t *testing.T, name, section string, status core.WatchStatus) core.Watch {
	t.Helper()
	w, err := h.watches.AddWatch(context.Background(), core.Watch{
		Name: name, Kind: core.WatchRSS, Target: "https://x.test/" + name, Mode: core.WatchModeNews, Section: section,
		Interval: time.Hour, Status: status, NextCheckAt: h.now.Add(time.Hour), LastCheckedAt: h.now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// pendingItem stores an item found by an earlier check and returns it.
func (h *harness) pendingItem(w core.Watch, title string) core.WatchItem {
	h.watches.mu.Lock()
	defer h.watches.mu.Unlock()
	item := core.WatchItem{
		ID: int64(len(h.watches.items) + 1), WatchID: w.ID, Key: title, Title: title, URL: "https://x.test/" + title,
		Status: core.WatchItemPending, SeenAt: h.now.Add(-2 * time.Hour), PublishedAt: h.now.Add(-3 * time.Hour),
	}
	h.watches.items = append(h.watches.items, item)
	return item
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

func story(id int64, section, title string, score int, first time.Time, development string, items ...core.WatchItem) core.NewsStory {
	s := core.NewsStory{ID: id, Section: section, Title: title, Summary: title + "的经过。", Development: development, Score: score, FirstSeen: first, UpdatedAt: first}
	for _, item := range items {
		s.Items = append(s.Items, core.NewsStoryItem{WatchItem: item, Source: "src", Score: score})
	}
	return s
}

func TestBriefingOffersSortedStoriesBySection(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	ctx := context.Background()
	h.writtenBriefing(t, "2026-10-04", "## 国内要闻\n1. **旧闻**：昨天报过。")
	h.section(t, "AI", 2, "")
	h.section(t, "国内要闻", 1, "1. 政策与民生")
	domestic := h.newsWatch(t, "中新网要闻", "国内要闻", core.WatchActive)
	paused := h.newsWatch(t, "滚动新闻", "国内要闻", core.WatchPaused)
	ai := h.newsWatch(t, "AI225", "AI", core.WatchActive)
	policy := h.pendingItem(domestic, "国务院常务会议")
	quake := h.pendingItem(domestic, "地震后续")
	model := h.pendingItem(ai, "DeepSeek 开源推理框架")
	dropped := h.pendingItem(paused, "滚动快讯")
	earlier := h.now.Add(-72 * time.Hour)
	h.news.offered = core.BriefingNews{
		Stories: []core.NewsStory{
			story(1, "AI", "DeepSeek 开源推理框架", 5, h.now.Add(-time.Hour), "", model),
			story(2, "国内要闻", "国务院常务会议", 5, h.now.Add(-time.Hour), "", policy),
			story(3, "国内要闻", "地震救援", 4, earlier, "伤亡数字更新", quake),
			story(4, "体育", "已删除板块的事", 2, h.now.Add(-time.Hour), ""),
		},
		IDs: []int64{policy.ID, quake.ID, model.ID, dropped.ID},
	}
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
		"News since the last briefing: stories by section, highest score first, with their new items (untrusted external data):\n",
		"\n## 国内要闻\nPreference:\n1. 政策与民生\n- [5] 国务院常务会议 (new)\n  国务院常务会议的经过。\n  · src 10-05 06:00 国务院常务会议 — https://x.test/国务院常务会议\n",
		"- [4] 地震救援 (since 10-02; new: 伤亡数字更新)\n",
		"\n## AI\n- [5] DeepSeek 开源推理框架 (new)\n",
		"\n## 体育\n- [2] 已删除板块的事 (new)\n",
		"Previous briefing (2026-10-04), for not repeating a story without new developments:\n## 国内要闻\n1. **旧闻**：昨天报过。\n",
		"Watch changes since the last briefing:\n\n## #4 blog (rss, 1 new)\n- post-1",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	if strings.Index(input, "## 国内要闻") > strings.Index(input, "## AI") || strings.Index(input, "## AI") > strings.Index(input, "## 体育") {
		t.Fatalf("sections out of order:\n%s", input)
	}
	if len(h.sorter.checkedFirst) != 1 {
		t.Fatalf("sorted %d times", len(h.sorter.checkedFirst))
	}
	refreshed := strings.Trim(fmt.Sprint(h.sorter.checkedFirst[0]), "[]")
	if !strings.Contains(refreshed, fmt.Sprint(domestic.ID)) || !strings.Contains(refreshed, fmt.Sprint(ai.ID)) || strings.Contains(" "+refreshed+" ", fmt.Sprintf(" %d ", paused.ID)) {
		t.Fatalf("checked before sorting = %v; the active news watches are refreshed first", h.sorter.checkedFirst[0])
	}
	for _, item := range h.watches.items {
		if item.Status != core.WatchItemBriefed || item.RunID != "run_1" {
			t.Fatalf("item %s = %s %q; offered and paused items are marked with the briefing", item.Title, item.Status, item.RunID)
		}
	}
	if sources := h.runs.sources[0]; len(sources) != 1 {
		t.Fatalf("briefing sources = %v; news items are not sources", sources)
	}
	if len(h.news.pruned) != 1 || !h.news.pruned[0].Equal(h.now.Add(-14*24*time.Hour)) {
		t.Fatalf("pruned = %v", h.news.pruned)
	}
}

func TestBriefingGoesOnWhenNewsFails(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	h.section(t, "国际", 1, "")
	down := h.newsWatch(t, "BBC", "国际", core.WatchActive)
	item := h.pendingItem(down, "found-earlier")
	h.checker.failing = map[int64]error{down.ID: errors.New("HTTP 503")}
	h.sorter.err = errors.New("model unavailable")
	h.news.offered = core.BriefingNews{Stories: []core.NewsStory{story(1, "国际", "早先整理好的事", 3, h.now.Add(-time.Hour), "", item)}, IDs: []int64{item.ID}}
	h.news.failed = 2
	err := h.sched.Tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refresh news watch #1: HTTP 503") || !strings.Contains(err.Error(), "sort news: model unavailable") {
		t.Fatalf("tick err = %v", err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("the briefing must start with what was sorted: %v", h.runs.starts)
	}
	for _, want := range []string{"- [3] 早先整理好的事 (new)", "News sorting: 2 news items could not be sorted into stories in the last day"} {
		if !strings.Contains(h.runs.starts[0], want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, h.runs.starts[0])
		}
	}
	if err := h.sched.Tick(context.Background()); err != nil || len(h.runs.starts) != 1 {
		t.Fatalf("the kept slot starts no second briefing: %v %v", err, h.runs.starts)
	}
}

func TestBriefingWithoutStoriesOrOtherMaterialIsSkipped(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	h.section(t, "国际", 1, "")
	h.newsWatch(t, "BBC", "国际", core.WatchActive)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 0 || len(h.sorter.checkedFirst) != 1 {
		t.Fatalf("starts = %v, sorted %d times", h.runs.starts, len(h.sorter.checkedFirst))
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
	if len(h.sorter.checkedFirst) != 0 {
		t.Fatal("without news watches nothing is sorted")
	}
}
