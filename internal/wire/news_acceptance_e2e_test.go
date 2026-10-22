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
)

// newsSources serves a news feed, a daily digest's feed and its issue pages
// from one private address.
type newsSources struct {
	mu     sync.Mutex
	base   string
	news   []string
	issues []string
	pages  map[string]string
}

func (s *newsSources) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.URL.Path == "/news":
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, newsFeed("中新网要闻", "http://news.test/n/", s.news))
	case r.URL.Path == "/daily":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, newsFeed("AI 日报", s.base+"/daily/", s.issues))
	case s.pages[r.URL.Path] != "":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, s.pages[r.URL.Path])
	default:
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}
}

// newsFeed lists keys newest first, each linked under prefix and dated an
// hour apart up to 2026-10-04 23:00 UTC.
func newsFeed(title, prefix string, keys []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>` + title + `</title>`)
	for i := len(keys) - 1; i >= 0; i-- {
		at := time.Date(2026, 10, 4, 23, 0, 0, 0, time.UTC).Add(time.Duration(i-len(keys)+1) * time.Hour)
		b.WriteString(`<item><title>` + keys[i] + `</title><link>` + prefix + keys[i] + `</link><guid>` + keys[i] + `</guid><pubDate>` + at.Format(time.RFC1123Z) + `</pubDate><description>关于` + keys[i] + `</description></item>`)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

func TestNewsSourcesFeedTheMorningBriefing(t *testing.T) {
	src := &newsSources{news: []string{"旧闻"}, issues: []string{"2026-10-04"}, pages: map[string]string{}}
	web := newPrivateNetworkServer(t, src)
	defer web.Close()
	src.base = web.URL
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC) // 20:00 on the 4th in Shanghai
	harness, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_s1", "news_sections", map[string]string{"action": "set", "name": "国内要闻", "preference": "1. 政策与民生"}),
		toolCallChunk("call_s2", "news_sections", map[string]string{"action": "set", "name": "AI 与科技前沿"}),
		toolCallChunk("call_news", "watch_create", map[string]any{"name": "中新网要闻", "kind": "rss", "target": web.URL + "/news", "mode": "news", "section": "国内要闻"}),
		toolCallChunk("call_daily", "watch_create", map[string]any{"name": "AI 日报", "kind": "rss", "target": web.URL + "/daily", "mode": "news", "section": "AI 与科技前沿", "issue": true}),
		textReply("配好了"),
		toolCallChunk("call_skill", "skill", map[string]string{"skill": "morning-briefing"}),
		toolCallChunk("call_push", "notify_owner", map[string]string{"title": "早安", "body": "1. 国务院常务会议"}),
		textReply("## 国内要闻\n1. **国务院常务会议**：部署民生工作。[中新网](http://news.test/n/国务院常务会议)\n\n## 今日关注\n1. 国务院常务会议"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+watchTestConfig)
	c := harness.open(t, cfg)
	defer c.Close()
	ctx := context.Background()
	registerPushInstallation(t, c, "fid_phone")

	thread, err := c.threads.CreateThread(ctx, "news")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.runs.CreateRun(ctx, thread.ID, "", "早上给我国内要闻和 AI 新闻", nil); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, "")
	if created := toolResult(t, provider.request(4), "call_daily"); !strings.Contains(created, `"baseline_items":1`) || !strings.Contains(created, `"issue":true`) {
		t.Fatalf("daily source = %s", created)
	}
	watches, err := c.store.ListWatches(ctx)
	if err != nil || len(watches) != 2 || watches[0].Section != "国内要闻" || watches[1].Mode != core.WatchModeNews || !watches[1].Issue {
		t.Fatalf("watches = %+v, %v", watches, err)
	}

	// Published after the sources' last check; only the refresh before the
	// briefing finds them.
	src.mu.Lock()
	src.news = append(src.news, "国务院常务会议", "高铁新线开通")
	src.issues = append(src.issues, "2026-10-05")
	src.pages["/daily/2026-10-05"] = `<html><head><title>AI 日报 2026-10-05</title></head><body><article><h1>AI 日报 2026-10-05</h1>` +
		`<h2>1. DeepSeek 开源推理框架 ★★★★★</h2><p>DeepSeek 开源了它在云上使用的推理框架，并公布了每天处理的 token 数。</p>` +
		`<h2>2. 谷歌开源 TorchTPU ★★★★</h2><p>开发者可以用原生 PyTorch 在 TPU 上训练与推理，Hugging Face 与 vLLM 同步支持。</p></article></body></html>`
	src.mu.Unlock()
	harness.clock.Set(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) // 08:00 in Shanghai
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	briefingThread, err := c.store.LatestRoutineThread(ctx, "briefing")
	if err != nil || briefingThread == "" {
		t.Fatalf("briefing thread = %q, %v", briefingThread, err)
	}
	waitNewRun(t, c, briefingThread, "")
	messages := requestMessages(provider.request(5))
	input := chatContentText(messages[len(messages)-3]["content"])
	for _, want := range []string{
		"[briefing 2026-10-05] morning briefing",
		"\n## 国内要闻\nPreference:\n1. 政策与民生\n\n### #1 中新网要闻 (2 new)\n- 10-05 07:00 高铁新线开通 — http://news.test/n/高铁新线开通\n  关于高铁新线开通\n- 10-05 06:00 国务院常务会议",
		"\n## AI 与科技前沿\n\n### #2 AI 日报 (1 new)\nIssue 10-05 07:00 2026-10-05 — " + web.URL + "/daily/2026-10-05\n--- issue page ---\n",
		"DeepSeek 开源推理框架 ★★★★★",
		"谷歌开源 TorchTPU",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	if strings.Contains(input, "旧闻") {
		t.Fatalf("a baseline item reached the briefing:\n%s", input)
	}
	if left, err := c.store.ListWatchItems(ctx, core.WatchItemPending, 10); err != nil || len(left) != 0 {
		t.Fatalf("news items left pending: %+v, %v", left, err)
	}
	if sent := harness.fcm.sent(); len(sent) != 1 {
		t.Fatalf("pushes = %v", sent)
	}
}
