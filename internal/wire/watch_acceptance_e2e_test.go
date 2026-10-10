package wire

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/watch"
)

// sources serves a feed, a price page and a GitHub API from one private
// address; tests change what they return.
type sources struct {
	mu       sync.Mutex
	feed     string
	price    string
	releases string
	// during runs once for a path while its request is served.
	during map[string]func()
}

func (s *sources) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hook := s.during[r.URL.Path]; hook != nil {
		delete(s.during, r.URL.Path)
		hook()
	}
	switch r.URL.Path {
	case "/feed":
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, s.feed)
	case "/phone":
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body><h1>Phone</h1><span class="price">`+s.price+`</span></body></html>`)
	case "/repos/golang/go/releases":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, s.releases)
	default:
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}
}

func (s *sources) set(update func(*sources)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s)
}

func rss(items ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Go blog</title>`)
	for _, title := range items {
		b.WriteString(`<item><title>` + title + `</title><link>https://go.dev/blog/` + title + `</link><guid>` + title + `</guid></item>`)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

const watchTestConfig = "owner:\n  timezone: Asia/Shanghai\nweb_access:\n  allow_private_networks: true\nbriefing:\n  at: \"08:00\"\n"

func TestWatchesWakeAndFeedTheMorningBriefing(t *testing.T) {
	src := &sources{feed: rss("go1.26"), price: "¥2,999"}
	web := newPrivateNetworkServer(t, src)
	defer web.Close()
	start := time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC) // 04:00 on the 5th in Shanghai
	harness, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_feed", "watch_create", map[string]string{"name": "Go blog", "kind": "rss", "target": web.URL + "/feed"}),
		toolCallChunk("call_price", "watch_create", map[string]string{"name": "phone price", "kind": "web", "target": web.URL + "/phone", "selector": ".price", "mode": "immediate"}),
		textReply("盯上了"),
		textReply("手机降价了"),
		toolCallChunk("call_skill", "skill", map[string]string{"skill": "skill.morning.briefing"}),
		toolCallChunk("call_note", "knowledge_write", map[string]any{"path": "briefings/2026-10-05.md", "title": "早安 2026-10-05", "tags": []string{"briefing"}, "body": "Go 博客有新文章 go1.27。"}),
		toolCallChunk("call_push", "notify_owner", map[string]string{"title": "早安", "body": "Go 博客有新文章"}),
		textReply("简报在 briefings/2026-10-05.md"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+watchTestConfig)
	installSeedSkill(t, cfg.Skills.Dir, "morning_briefing")
	c := harness.open(t, cfg)
	defer c.Close()
	ctx := context.Background()
	registerPushToken(t, c, "fcm_phone")

	thread, err := c.threads.CreateThread(ctx, "watch")
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.runs.CreateRun(ctx, thread.ID, "", "帮我盯着 Go 博客，手机降价马上告诉我")
	if err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, "")
	if created := toolResult(t, provider.request(1), "call_feed"); !strings.Contains(created, `"baseline_items":1`) {
		t.Fatalf("feed watch = %s", created)
	}
	watches, err := c.store.ListWatches(ctx)
	if err != nil || len(watches) != 2 || watches[1].Mode != core.WatchModeImmediate || watches[1].SessionID != thread.ID {
		t.Fatalf("watches = %+v, %v", watches, err)
	}

	src.set(func(s *sources) { s.feed, s.price = rss("go1.26", "go1.27"), "¥2,799" })
	harness.clock.Set(start.Add(time.Hour + time.Minute))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	woken := waitNewRun(t, c, thread.ID, run.ID)
	wakeInput := requestMessages(provider.request(3))
	if input := chatContentText(wakeInput[len(wakeInput)-3]["content"]); !strings.HasPrefix(input, "[watch #2 phone price] 1 new") || !strings.Contains(input, "Before: ¥2,999\n  Now: ¥2,799") {
		t.Fatalf("watch wake input = %q", input)
	}
	if wakes, err := c.store.CountWakesSince(ctx, time.Time{}); err != nil || wakes != 1 {
		t.Fatalf("watch wakes = %d, %v", wakes, err)
	}
	pending, err := c.store.ListWatchItems(ctx, core.WatchItemPending, 10)
	if err != nil || len(pending) != 1 || pending[0].Title != "go1.27" {
		t.Fatalf("pending = %+v, %v", pending, err)
	}

	harness.clock.Set(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) // 08:00 in Shanghai
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	briefingThread, err := c.store.LatestRoutineThread(ctx, "briefing")
	if err != nil || briefingThread == "" {
		t.Fatalf("briefing thread = %q, %v", briefingThread, err)
	}
	waitNewRun(t, c, briefingThread, woken.RunID)
	briefingInput := requestMessages(provider.request(4))
	if input := chatContentText(briefingInput[len(briefingInput)-3]["content"]); !strings.HasPrefix(input, "[briefing 2026-10-05] morning briefing") || !strings.Contains(input, "## #1 Go blog (rss, 1 new)\n- go1.27 — https://go.dev/blog/go1.27") {
		t.Fatalf("briefing input = %q", input)
	}
	if _, err := c.store.KnowledgeNote(ctx, "briefings/2026-10-05.md"); err != nil {
		t.Fatalf("briefing note: %v", err)
	}
	sent := harness.fcm.sent()
	if len(sent) != 1 {
		t.Fatalf("pushes = %v", sent)
	}
	if data, _ := sent[0]["data"].(map[string]any); data["thread_id"] != briefingThread {
		t.Fatalf("push data = %v, want the briefings thread %s", data, briefingThread)
	}
	if left, _ := c.store.ListWatchItems(ctx, core.WatchItemPending, 10); len(left) != 0 {
		t.Fatalf("items still pending after the briefing: %+v", left)
	}

	requests := provider.requestCount()
	harness.clock.Set(time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if provider.requestCount() != requests {
		t.Fatal("the briefing ran twice on one day")
	}
}

func TestBriefingListsReleasesAndFailingWatches(t *testing.T) {
	src := &sources{releases: `[]`}
	web := newPrivateNetworkServer(t, src)
	defer web.Close()
	start := time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC)
	harness, extra := newPushHarness(t, start)
	harness.opts.githubAPI = web.URL
	provider := &fakeOpenAI{replies: []string{textReply("早")}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+watchTestConfig)
	c := harness.open(t, cfg)
	defer c.Close()
	ctx := context.Background()
	add := func(w core.Watch) {
		w.Mode, w.Interval, w.Status, w.NextCheckAt, w.CreatedAt = core.WatchModeDigest, time.Hour, core.WatchActive, start, start
		if _, err := c.store.AddWatch(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	add(core.Watch{Name: "Go releases", Kind: core.WatchGitHub, Target: "golang/go", Selector: "releases"})
	add(core.Watch{Name: "shop", Kind: core.WatchWeb, Target: web.URL + "/gone", Failures: watch.FailingAfter - 1, LastCheckedAt: start.Add(-time.Hour)})
	if err := c.wake.Tick(ctx); err == nil {
		t.Fatal("the broken watch must report its failure")
	}

	src.set(func(s *sources) {
		s.releases = `[{"id":42,"tag_name":"go1.27","name":"Go 1.27","html_url":"https://github.com/golang/go/releases/tag/go1.27","body":"Generic methods."}]`
	})
	harness.clock.Set(start.Add(time.Hour + time.Minute))
	_ = c.wake.Tick(ctx) // the shop watch is still failing

	harness.clock.Set(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	_ = c.wake.Tick(ctx)
	briefingThread, err := c.store.LatestRoutineThread(ctx, "briefing")
	if err != nil || briefingThread == "" {
		t.Fatalf("briefing thread = %q, %v", briefingThread, err)
	}
	waitNewRun(t, c, briefingThread, "")
	messages := requestMessages(provider.request(0))
	input := chatContentText(messages[len(messages)-3]["content"])
	for _, want := range []string{"## #1 Go releases (github, 1 new)\n- Go 1.27 — https://github.com/golang/go/releases/tag/go1.27\n  Generic methods.", "Failing watches:\n- #2 shop: web fetch returned HTTP status 500"} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
}

func TestOwnerEditsDuringACheckStand(t *testing.T) {
	src := &sources{feed: rss("go1.26")}
	web := newPrivateNetworkServer(t, src)
	defer web.Close()
	start := time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC)
	harness, extra := newPushHarness(t, start)
	server := httptest.NewServer(&fakeOpenAI{})
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+watchTestConfig)
	c := harness.open(t, cfg)
	defer c.Close()
	ctx := context.Background()
	add := func(w core.Watch) core.Watch {
		w.Mode, w.Interval, w.NextCheckAt, w.LastCheckedAt, w.CreatedAt = core.WatchModeImmediate, time.Hour, start, start.Add(-time.Hour), start
		added, err := c.store.AddWatch(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		return added
	}
	feed := add(core.Watch{Name: "Go blog", Kind: core.WatchRSS, Target: web.URL + "/feed", Status: core.WatchActive})
	shop := add(core.Watch{Name: "shop", Kind: core.WatchWeb, Target: web.URL + "/gone", Status: core.WatchFailing, Failures: watch.FailingAfter + 1, LastError: "HTTP 500"})
	// edit changes a watch the way watch_update does.
	edit := func(id int64, change func(*core.Watch)) {
		loaded, err := c.store.LoadWatch(ctx, id)
		if err != nil {
			t.Error(err)
			return
		}
		change(loaded)
		loaded.UpdatedAt = start
		if err := c.store.UpdateWatch(ctx, *loaded); err != nil {
			t.Error(err)
		}
	}
	src.set(func(s *sources) {
		s.feed = rss("go1.26", "go1.27")
		s.during = map[string]func(){
			"/feed": func() { edit(feed.ID, func(w *core.Watch) { w.Status = core.WatchPaused }) },
			"/gone": func() {
				edit(shop.ID, func(w *core.Watch) {
					w.Status, w.Failures, w.LastError, w.NextCheckAt = core.WatchActive, 0, "", start
				})
			},
		}
	})
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	paused, err := c.store.LoadWatch(ctx, feed.ID)
	if err != nil || paused.Status != core.WatchPaused {
		t.Fatalf("paused during its check = %+v, %v", paused, err)
	}
	resumed, err := c.store.LoadWatch(ctx, shop.ID)
	if err != nil || resumed.Status != core.WatchActive || resumed.Failures != 0 || resumed.LastError != "" || !resumed.NextCheckAt.Equal(start) {
		t.Fatalf("resumed during its check = %+v, %v", resumed, err)
	}
	due, err := c.store.ListDueWatches(ctx, start.Add(48*time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].ID != shop.ID {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if pending, err := c.store.ListWatchItems(ctx, core.WatchItemPending, 10); err != nil || len(pending) != 0 {
		t.Fatalf("a discarded check left items = %+v, %v", pending, err)
	}
	if runs, err := c.store.CountWakesSince(ctx, time.Time{}); err != nil || runs != 0 {
		t.Fatalf("wakes = %d, %v", runs, err)
	}
}
