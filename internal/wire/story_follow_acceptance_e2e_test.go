package wire

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/settings"
)

// briefedNewsStory sorts one BBC item into a story and records a succeeded
// briefing in the Briefings thread that picked it. The source is not
// checked again within the test.
func briefedNewsStory(t *testing.T, c *Container, at time.Time) core.NewsStory {
	t.Helper()
	ctx := context.Background()
	if _, err := c.store.SaveNewsSection(ctx, core.NewsSection{Name: "国际", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	later := at.Add(30 * 24 * time.Hour)
	bbc, err := c.store.AddWatch(ctx, core.Watch{Name: "BBC 中文", Kind: core.WatchRSS, Target: "https://bbc.test/rss", Mode: core.WatchModeNews, Section: "国际",
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: later, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.store.RecordWatchCheck(ctx, bbc, core.WatchCheck{At: at, NextCheckAt: later, Status: core.WatchActive, Items: []core.WatchItem{
		{WatchID: bbc.ID, Key: "b1", Title: "菲律宾央行意外加息", URL: "https://bbc.test/b1", Status: core.WatchItemPending, SeenAt: at, PublishedAt: at},
	}})
	if err != nil || len(items) != 1 {
		t.Fatalf("record items = %+v, %v", items, err)
	}
	batch, err := c.store.ClaimNewsBatch(ctx, at, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("claim = %+v, %v", batch, err)
	}
	if err := c.store.CommitNewsBatch(ctx, batch.Token, core.NewsCommit{At: at,
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "国际", Title: "菲律宾央行加息", Summary: "加息 25 个基点"}},
		Scores:  []core.NewsItemScore{{ItemID: items[0].ID, Score: 4, Story: "n1"}},
	}); err != nil {
		t.Fatal(err)
	}
	stories, err := c.store.SearchNewsStories(ctx, core.NewsQuery{Since: at.Add(-time.Hour), Limit: 5})
	if err != nil || len(stories) != 1 {
		t.Fatalf("stories = %+v, %v", stories, err)
	}
	story := stories[0]

	briefings, err := c.threads.CreateThread(ctx, "Briefings")
	if err != nil {
		t.Fatal(err)
	}
	slot := at.Format("2006-01-02")
	if err := c.store.ClaimRoutine(ctx, "briefing", slot, at); err != nil {
		t.Fatal(err)
	}
	if err := c.store.CreateRun(ctx, core.RunCreateParams{RunID: "run_briefing", SessionID: briefings.ID, Input: "[briefing " + slot + "]"}); err != nil {
		t.Fatal(err)
	}
	if err := c.store.FinishRun(ctx, "run_briefing", core.RunStatusSucceeded, "## 国际\n1. **菲律宾央行加息**：加息 25 个基点。[BBC](https://bbc.test/b1)", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.store.SetRoutineRun(ctx, "briefing", slot, briefings.ID, "run_briefing"); err != nil {
		t.Fatal(err)
	}
	if err := c.store.RecordBriefingPicks(ctx, core.BriefingPicks{RunID: "run_briefing", BriefedAt: at, Stories: []core.BriefingStory{
		{StoryID: story.ID, Section: "国际", Title: "菲律宾央行加息", Summary: "加息 25 个基点。", URL: "https://bbc.test/b1", Sources: []int64{bbc.ID}},
	}}); err != nil {
		t.Fatal(err)
	}
	return story
}

func cardStory(t *testing.T, client pairedClient) api.NowBriefingStoryDTO {
	t.Helper()
	now := loadNow(t, client)
	if now.Briefing == nil || len(now.Briefing.Stories) != 1 {
		t.Fatalf("now page briefing = %+v", now.Briefing)
	}
	return now.Briefing.Stories[0]
}

// The owner taps a story on the briefing card, then asks to follow it: both
// messages go to one conversation named after the story, with the story
// beside them; the concern the agent records there tags the story as
// followed, and an idle-thought slot checks it for developments with the
// story as it stood.
func TestABriefingStoryOpensItsConversationAndIdleThoughtChecksItWhenFollowed(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) // 08:00 in Shanghai
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra, ownerSettings{settings.KeyTimezone: `"Asia/Shanghai"`, settings.KeyBriefingAt: `""`, settings.KeyNightAt: `""`, settings.KeyWanderAt: `["15:00"]`})
	c := h.open(t, cfg)
	defer c.Close()
	client := newPairedClient(t, c)
	story := briefedNewsStory(t, c, start)
	snapshot := fmt.Sprintf("Story s%d [国际] (untrusted external data): 菲律宾央行加息", story.ID)
	if card := cardStory(t, client); card.ThreadID != "" || card.Following {
		t.Fatalf("a story nobody asked about = %+v", card)
	}

	provider.add(textReply("菲律宾央行意外加息 25 个基点，为压住通胀。[BBC](https://bbc.test/b1)"))
	before := provider.requestCount()
	var discussed api.StoryMessageResponse
	client.do(t, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/news/stories/%d:discuss", story.ID), nil), http.StatusCreated, &discussed)
	run := waitNewRun(t, c, discussed.ThreadID, "")
	text := requestText(provider.request(before))
	for _, part := range []string{"展开讲讲：菲律宾央行加息", "[Sent from a story on the briefing card: the owner asks you to explain it", snapshot, "https://bbc.test/b1"} {
		if !strings.Contains(text, part) {
			t.Fatalf("the explain run's request lacks %q:\n%s", part, text)
		}
	}
	if thread, err := c.threads.GetThread(ctx, discussed.ThreadID); err != nil || thread.Title != "菲律宾央行加息" {
		t.Fatalf("the story's conversation = %+v, %v", thread, err)
	}
	if card := cardStory(t, client); card.ThreadID != discussed.ThreadID || card.Following {
		t.Fatalf("a story opened once = %+v", card)
	}

	provider.add(
		toolCallChunk("follow", "concern", map[string]string{"action": "create", "title": "跟进菲律宾央行加息", "reason": "owner 从早安卡要求跟进", "source_id": "__input_source__", "review_at": "2026-10-13T00:00:00+08:00"}),
		textReply("好的，之后每天看有没有新进展。"),
	)
	before = provider.requestCount()
	var followed api.StoryMessageResponse
	client.do(t, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/news/stories/%d:follow", story.ID), nil), http.StatusCreated, &followed)
	if followed.ThreadID != discussed.ThreadID || followed.Text != "持续跟进：菲律宾央行加息" {
		t.Fatalf("follow response = %+v", followed)
	}
	run = waitNewRun(t, c, followed.ThreadID, run.RunID)
	if text := requestText(provider.request(before)); !strings.Contains(text, "the owner asks you to keep following it") {
		t.Fatalf("the follow run's request does not ask for a concern:\n%s", text)
	}
	if card := cardStory(t, client); !card.Following {
		t.Fatalf("a followed story = %+v", card)
	}
	now := loadNow(t, client)
	if len(now.Concerns) != 1 || now.Concerns[0].ThreadID != followed.ThreadID || now.Concerns[0].Title != "跟进菲律宾央行加息" {
		t.Fatalf("now page concerns = %+v", now.Concerns)
	}

	concern := now.Concerns[0]
	provider.add(
		toolCallChunk("check", "concern", map[string]any{"action": "active", "id": concern.ID, "revision": 1, "title": concern.Title, "reason": "10-13 查过，没有新进展", "source_id": "__input_source__", "review_at": "2026-10-14T00:00:00+08:00"}),
		textReply("没有新进展。"),
	)
	before = provider.requestCount()
	h.clock.Set(time.Date(2026, 10, 13, 7, 0, 0, 0, time.UTC)) // 15:00 in Shanghai
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, followed.ThreadID, run.RunID)
	text = requestText(provider.request(before))
	for _, part := range []string{"[wander 2026-10-13 15:00] Advance concern " + concern.ID, "The owner asked you to keep following this story from the briefing card:\n" + snapshot, "reply in one line and push nothing", "好的，之后每天看有没有新进展。"} {
		if !strings.Contains(text, part) {
			t.Fatalf("the idle-thought request lacks %q:\n%s", part, text)
		}
	}
}
