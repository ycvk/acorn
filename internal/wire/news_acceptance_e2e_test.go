package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
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

// approveNext waits for a pending action not in approved and accepts it as
// the app does, returning the summary and the detail the owner saw.
func approveNext(t *testing.T, c *Container, approved map[string]bool) string {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	for {
		actions, err := c.pendingAction.List(ctx, 10)
		if err != nil {
			t.Fatalf("list pending actions: %v", err)
		}
		for _, a := range actions {
			if approved[a.ActionID] {
				continue
			}
			detail, err := c.pendingAction.Get(ctx, a.ActionID)
			if err != nil {
				t.Fatalf("pending action %s: %v", a.ActionID, err)
			}
			if _, err := c.pendingAction.Decide(ctx, a.ActionID, api.PendingActionDecisionInput{Decision: "accept"}); err != nil {
				t.Fatalf("approve %s: %v", a.ActionID, err)
			}
			approved[a.ActionID] = true
			return detail.ToolSummary + "\n" + detail.ToolDetail
		}
		if time.Now().After(deadline) {
			t.Fatalf("no new pending action; approved %v", approved)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// completionReply is a JSON chat completion with text, for calls that do not
// stream.
func completionReply(text string) string {
	reply, err := json.Marshal(map[string]any{
		"id": "c", "object": "chat.completion", "created": 1, "model": "fake",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 900, "completion_tokens": 100, "total_tokens": 1000},
	})
	if err != nil {
		panic(err)
	}
	return string(reply)
}

// newsItemIDs replaces __item:<title>__ in reply with the id the news sorting
// input in messages gives the item or issue titled <title>.
func newsItemIDs(reply string, messages []map[string]any) string {
	for _, msg := range messages {
		if msg["role"] != "user" {
			continue
		}
		for _, line := range strings.Split(chatContentText(msg["content"]), "\n") {
			if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "Issue ") {
				continue
			}
			parts := strings.Split(line, " | ")
			head := strings.Fields(parts[0])
			if len(parts) < 2 || len(head) < 2 {
				continue
			}
			reply = strings.ReplaceAll(reply, "__item:"+parts[1]+"__", head[1])
		}
	}
	return reply
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
		toolCallChunk("call_p1", "news_source", map[string]any{"action": "promote", "id": 1, "reason": "你要的国内要闻"}),
		toolCallChunk("call_p2", "news_source", map[string]any{"action": "promote", "id": 2, "reason": "你要的 AI 新闻"}),
		textReply("配好了"),
		completionReply(`{"items":[],"entries":[` +
			`{"issue":__item:2026-10-05__,"title":"DeepSeek 开源推理框架","summary":"DeepSeek 开源了云上使用的推理框架。","score":5,"story":"n1"},` +
			`{"issue":__item:2026-10-05__,"title":"谷歌开源 TorchTPU","summary":"PyTorch 可以直接在 TPU 上训练。","url":"/daily/torchtpu","score":4,"story":"n2"}],` +
			`"stories":[{"ref":"n1","section":"AI 与科技前沿","title":"DeepSeek 开源推理框架","summary":"DeepSeek 开源了它在云上使用的推理框架。"},` +
			`{"ref":"n2","section":"AI 与科技前沿","title":"谷歌开源 TorchTPU","summary":"谷歌开源 TorchTPU，PyTorch 可以直接在 TPU 上训练与推理。"}]}`),
		completionReply(`{"items":[{"id":__item:国务院常务会议__,"score":5,"story":"n1"},{"id":__item:高铁新线开通__,"score":3,"story":"n2"}],` +
			`"stories":[{"ref":"n1","section":"国内要闻","title":"国务院常务会议","summary":"国务院常务会议部署民生工作。"},{"ref":"n2","section":"国内要闻","title":"高铁新线开通","summary":"一条高铁新线开通运营。"}]}`),
		toolCallChunk("call_skill", "skill", map[string]string{"skill": "morning-briefing"}),
		textReply("## 国内要闻\n1. **国务院常务会议**：部署民生工作。[中新网](http://news.test/n/国务院常务会议)\n\n## 今日关注\n- 第 1 条：民生工作的部署。"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+watchTestConfig, watchTestSettings)
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
	// New sources start on trial; the owner approves taking each in.
	approved := map[string]bool{}
	for _, want := range []string{
		"把新闻源「中新网要闻」（板块：国内要闻）转正，从下一份早安卡起采用它。理由：你要的国内要闻\n把新闻源「中新网要闻」",
		"把新闻源「AI 日报」（板块：AI 与科技前沿）转正，从下一份早安卡起采用它。理由：你要的 AI 新闻\n把新闻源「AI 日报」",
	} {
		if detail := approveNext(t, c, approved); !strings.HasPrefix(detail, want) {
			t.Fatalf("approval detail = %q, want %q…", detail, want)
		}
	}
	waitNewRun(t, c, thread.ID, "")
	if created := toolResult(t, provider.request(4), "call_daily"); !strings.Contains(created, `"baseline_items":1`) || !strings.Contains(created, `"issue":true`) || !strings.Contains(created, `"trial":true`) {
		t.Fatalf("daily source = %s", created)
	}
	watches, err := c.store.ListWatches(ctx)
	if err != nil || len(watches) != 2 || watches[0].Section != "国内要闻" || watches[1].Mode != core.WatchModeNews || !watches[1].Issue || watches[0].Trial || watches[1].Trial {
		t.Fatalf("watches = %+v, %v", watches, err)
	}

	// Published after the sources' last check; only the refresh before the
	// briefing finds them, and the catch-up sorts them into stories.
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
	issueInput := chatContentText(requestMessages(provider.request(7))[1]["content"])
	for _, want := range []string{"Sections:\n## 国内要闻\nPreference:\n1. 政策与民生\n## AI 与科技前沿\n", "\n--- issue page ---\n", "DeepSeek 开源推理框架 ★★★★★", "谷歌开源 TorchTPU"} {
		if !strings.Contains(issueInput, want) {
			t.Fatalf("issue sorting input lacks %q:\n%s", want, issueInput)
		}
	}
	if itemsInput := chatContentText(requestMessages(provider.request(8))[1]["content"]); !strings.Contains(itemsInput, "[AI 与科技前沿] DeepSeek 开源推理框架: DeepSeek 开源了它在云上使用的推理框架。") || strings.Contains(itemsInput, "旧闻") {
		t.Fatalf("items sorting input:\n%s", itemsInput)
	}
	messages := requestMessages(provider.request(9))
	input := chatContentText(messages[len(messages)-3]["content"])
	for _, want := range []string{
		"[briefing 2026-10-05] morning briefing",
		"\n## 国内要闻\nPreference:\n1. 政策与民生\n- [5] 国务院常务会议 (new)\n  国务院常务会议部署民生工作。\n  · 中新网要闻 10-05 06:00 国务院常务会议 — http://news.test/n/国务院常务会议\n- [3] 高铁新线开通 (new)\n",
		"\n## AI 与科技前沿\n- [5] DeepSeek 开源推理框架 (new)\n  DeepSeek 开源了它在云上使用的推理框架。\n  · AI 日报 10-05 07:00 DeepSeek 开源推理框架 — " + web.URL + "/daily/2026-10-05\n",
		"  · AI 日报 10-05 07:00 谷歌开源 TorchTPU — " + web.URL + "/daily/torchtpu",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	if strings.Contains(input, "旧闻") || strings.Contains(input, "issue page") {
		t.Fatalf("a baseline item or an issue page reached the briefing:\n%s", input)
	}
	if left, err := c.store.ListWatchItems(ctx, core.WatchItemPending, 10); err != nil || len(left) != 0 {
		t.Fatalf("news items left pending: %+v, %v", left, err)
	}
	if used, err := c.store.MemoryUsageSince(ctx, start, "news"); err != nil || used != 2000 {
		t.Fatalf("news usage = %d, %v", used, err)
	}
	briefingPushes := func() []string {
		var bodies []string
		for _, push := range harness.fcm.sent() {
			if n, _ := push["notification"].(map[string]any); n != nil && n["title"] == "早安" {
				bodies = append(bodies, fmt.Sprint(n["body"]))
			}
		}
		return bodies
	}
	if pushed := briefingPushes(); len(pushed) != 0 {
		t.Fatalf("the briefing run pushed itself: %q", pushed)
	}

	// The next tick reads the stories the briefing picked from its reply and
	// pushes today's focus; the now page lists them and the owner reacts to
	// one.
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if pushed := briefingPushes(); len(pushed) != 1 || pushed[0] != "国务院常务会议" {
		t.Fatalf("briefing pushes = %q: %v", pushed, harness.fcm.sent())
	}
	client := newPairedClient(t, c)
	now := loadNow(t, client)
	if now.Briefing == nil || len(now.Briefing.Stories) != 1 {
		t.Fatalf("briefing = %+v", now.Briefing)
	}
	picked := now.Briefing.Stories[0]
	want := api.NowBriefingStoryDTO{StoryID: picked.StoryID, Section: "国内要闻", Title: "国务院常务会议", Summary: "部署民生工作。", URL: "http://news.test/n/国务院常务会议", Focus: true}
	if picked != want {
		t.Fatalf("picked story = %+v", picked)
	}
	react := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/news/stories/%d/reaction", picked.StoryID), strings.NewReader(`{"reaction":"less"}`))
	react.Header.Set("Content-Type", "application/json")
	client.do(t, react, http.StatusNoContent, nil)
	if now := loadNow(t, client); now.Briefing.Stories[0].Reaction != "less" {
		t.Fatalf("story after the reaction = %+v", now.Briefing.Stories[0])
	}
	stats, err := c.store.NewsSourceStats(ctx, start)
	if err != nil || len(stats) != 2 || stats[0].Watch.Name != "中新网要闻" || stats[0].Picked != 1 || stats[0].Focus != 1 || stats[0].Less != 1 || stats[1].Picked != 0 {
		t.Fatalf("source stats = %+v, %v", stats, err)
	}
}
