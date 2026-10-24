package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func newsStoryFixture(t *testing.T) (*Store, core.Watch, core.Watch, core.Watch) {
	t.Helper()
	s := openTestStore(t)
	ctx := context.Background()
	for i, name := range []string{"国际", "AI"} {
		if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: name, Position: i, UpdatedAt: watchTestNow}); err != nil {
			t.Fatal(err)
		}
	}
	bbc := addNewsWatch(t, s, "bbc", "国际", false)
	ft := addNewsWatch(t, s, "ft", "国际", false)
	daily := addNewsWatch(t, s, "daily", "AI", true)
	return s, bbc, ft, daily
}

func TestNewsBatchesSortItemsIntoStoriesOnce(t *testing.T) {
	s, bbc, ft, daily := newsStoryFixture(t)
	ctx := context.Background()
	recorded := recordItems(t, s, bbc, "told", "b1", "b2")
	told, bbcItems := recorded[0], recorded[1:]
	if err := s.MarkWatchItems(ctx, []int64{told.ID}, core.WatchItemBriefed, "run_0"); err != nil {
		t.Fatal(err)
	}
	ftItems := recordItems(t, s, ft, "f1")
	issues := recordItems(t, s, daily, "2026-10-03")

	batch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 3 || batch.Items[0].Source != "bbc" || batch.Items[0].Section != "国际" || batch.Items[0].Issue {
		t.Fatalf("batch = %+v, %v", batch, err)
	}
	if again, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false); err != nil || len(again.Items) != 0 {
		t.Fatalf("claimed items were claimed again: %+v, %v", again, err)
	}
	issueBatch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, true)
	if err != nil || len(issueBatch.Items) != 1 || !issueBatch.Items[0].Issue {
		t.Fatalf("issue batch = %+v, %v", issueBatch, err)
	}

	commit := core.NewsCommit{
		At:      watchTestNow.Add(time.Minute),
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "国际", Title: "停火谈判", Summary: "双方开始谈判。"}},
		Scores: []core.NewsItemScore{
			{ItemID: bbcItems[0].ID, Score: 4, Story: "n1"},
			{ItemID: ftItems[0].ID, Score: 5, Story: "n1"},
		},
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, commit); err == nil || !strings.Contains(err.Error(), "2 of 3 claimed items") {
		t.Fatalf("a commit that leaves an item unscored err = %v", err)
	}
	commit.Scores = append(commit.Scores, core.NewsItemScore{ItemID: bbcItems[1].ID, Score: 1})
	if err := s.CommitNewsBatch(ctx, batch.Token, commit); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, commit); !errors.Is(err, core.ErrNewsLeaseLost) {
		t.Fatalf("a second commit of the batch err = %v", err)
	}

	issue := core.NewsCommit{
		At:      watchTestNow.Add(2 * time.Minute),
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "AI", Title: "DeepSeek 开源推理框架", Summary: "开源了推理框架。"}},
		Scores:  []core.NewsItemScore{{ItemID: issues[0].ID}},
		Entries: []core.NewsIssueEntry{
			{IssueID: issues[0].ID, Title: "DeepSeek 开源推理框架", URL: "https://a.test/1", Summary: "推理框架", Score: 5, Story: "n1"},
			{IssueID: issues[0].ID, Title: "停火谈判与芯片", Summary: "同一件事的另一家报道", Score: 3, Story: "s1"},
		},
	}
	if err := s.CommitNewsBatch(ctx, issueBatch.Token, issue); err != nil {
		t.Fatal(err)
	}

	news, err := s.BriefingNewsStories(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(news.Stories) != 2 || news.Stories[0].Section != "AI" || news.Stories[1].Title != "停火谈判" {
		t.Fatalf("stories = %+v", news.Stories)
	}
	ceasefire := news.Stories[1]
	if ceasefire.Score != 5 || len(ceasefire.Items) != 3 || !slices.ContainsFunc(ceasefire.Items, func(i core.NewsStoryItem) bool { return i.Source == "daily" && i.Key == "2026-10-03#2" }) {
		t.Fatalf("ceasefire = %+v", ceasefire)
	}
	// Two entries, the issue itself and the unsorted b2 (score 1, no story) are all marked.
	if len(news.IDs) != 6 {
		t.Fatalf("ids = %v", news.IDs)
	}
	if again, err := s.ClaimNewsBatch(ctx, watchTestNow.Add(time.Hour), time.Minute, 10, false); err != nil || len(again.Items) != 0 {
		t.Fatalf("sorted items or issue entries were claimed: %+v, %v", again, err)
	}

	found, err := s.SearchNewsStories(ctx, core.NewsQuery{Text: "DeepSeek", Since: watchTestNow, Limit: 5})
	if err != nil || len(found) != 1 || found[0].Items[0].URL != "https://a.test/1" {
		t.Fatalf("search = %+v, %v", found, err)
	}
	if found, err := s.SearchNewsStories(ctx, core.NewsQuery{Section: "国际", Since: watchTestNow, Limit: 5}); err != nil || len(found) != 1 || found[0].Title != "停火谈判" {
		t.Fatalf("section search = %+v, %v", found, err)
	}
	if found, err := s.SearchNewsStories(ctx, core.NewsQuery{Text: " 谈判  芯片 ", Since: watchTestNow, Limit: 5}); err != nil || len(found) != 1 || found[0].Title != "停火谈判" {
		t.Fatalf("every word matches in the story or its items: %+v, %v", found, err)
	}
	for _, text := range []string{"谈判 DeepSeek", "%", "停_谈判"} {
		if found, err := s.SearchNewsStories(ctx, core.NewsQuery{Text: text, Since: watchTestNow, Limit: 5}); err != nil || len(found) != 0 {
			t.Fatalf("search %q = %+v, %v", text, found, err)
		}
	}
	recent, err := s.RecentNewsStories(ctx, []string{"国际"}, watchTestNow, 10)
	if err != nil || len(recent) != 1 || recent[0].ID != ceasefire.ID {
		t.Fatalf("recent = %+v, %v", recent, err)
	}
}

func TestNewsBatchesWaitOutTheBudgetAndGiveUpAfterThreeFailures(t *testing.T) {
	s, bbc, _, _ := newsStoryFixture(t)
	ctx := context.Background()
	recordItems(t, s, bbc, "b1")
	batch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 1 {
		t.Fatal(batch, err)
	}
	tomorrow := watchTestNow.Add(24 * time.Hour)
	if err := s.DeferNewsBatch(ctx, batch.Token, tomorrow); err != nil {
		t.Fatal(err)
	}
	if early, err := s.ClaimNewsBatch(ctx, tomorrow.Add(-time.Second), time.Minute, 10, false); err != nil || len(early.Items) != 0 {
		t.Fatalf("a deferred item was claimed before its day: %+v, %v", early, err)
	}
	at := tomorrow
	for attempt := 1; attempt <= 3; attempt++ {
		batch, err = s.ClaimNewsBatch(ctx, at, time.Minute, 10, false)
		if err != nil || len(batch.Items) != 1 {
			t.Fatalf("attempt %d: %+v, %v", attempt, batch, err)
		}
		if err := s.FailNewsBatch(ctx, batch.Token, fmt.Sprintf("bad JSON %d", attempt), at); err != nil {
			t.Fatal(err)
		}
		at = at.Add(time.Minute)
	}
	if left, err := s.ClaimNewsBatch(ctx, at, time.Minute, 10, false); err != nil || len(left.Items) != 0 {
		t.Fatalf("an item failed three times is claimed again: %+v, %v", left, err)
	}
	failing, err := s.FailingNewsSources(ctx)
	if err != nil || len(failing) != 1 || failing[0].Kind != "bbc" || failing[0].Failed != 1 || failing[0].LastError != "bad JSON 3" || !failing[0].Since.Equal(tomorrow.Add(2*time.Minute)) {
		t.Fatalf("failing = %+v, %v", failing, err)
	}
	if news, err := s.BriefingNewsStories(ctx, 10); err != nil || len(news.Stories) != 0 || len(news.IDs) != 1 {
		t.Fatalf("a failed item is dropped without a story: %+v, %v", news, err)
	}
}

func TestNewsSourcesFailUntilTheSortingSucceedsAgain(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	giveUp := func(w core.Watch, key string, at time.Time) {
		t.Helper()
		recordItems(t, s, w, key)
		for range 3 {
			batch, err := s.ClaimNewsBatch(ctx, at, time.Minute, 10, false)
			if err != nil || len(batch.Items) != 1 {
				t.Fatalf("claim %s: %+v, %v", key, batch, err)
			}
			if err := s.FailNewsBatch(ctx, batch.Token, "model unavailable", at); err != nil {
				t.Fatal(err)
			}
			at = at.Add(time.Minute)
		}
	}
	giveUp(bbc, "b1", watchTestNow)
	giveUp(ft, "f1", watchTestNow)
	loaded, err := s.LoadWatch(ctx, ft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateWatch(ctx, loaded.Pause(watchTestNow.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if failing, err := s.FailingNewsSources(ctx); err != nil || len(failing) != 1 || failing[0].Kind != "bbc" {
		t.Fatalf("failing = %+v, %v; a paused source is left out", failing, err)
	}

	current, err := s.LoadWatch(ctx, bbc.ID)
	if err != nil {
		t.Fatal(err)
	}
	sorted := recordItems(t, s, *current, "b2")
	batch, err := s.ClaimNewsBatch(ctx, watchTestNow.Add(2*time.Hour), time.Minute, 10, false)
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("claim b2: %+v, %v", batch, err)
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, core.NewsCommit{
		At:      watchTestNow.Add(2 * time.Hour),
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "国际", Title: "停火谈判"}},
		Scores:  []core.NewsItemScore{{ItemID: sorted[0].ID, Score: 3, Story: "n1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if failing, err := s.FailingNewsSources(ctx); err != nil || len(failing) != 0 {
		t.Fatalf("failing = %+v, %v; the source sorted an item since", failing, err)
	}
}

func TestPruneNewsKeepsKeysAndItemsThatAreMemorySources(t *testing.T) {
	s, bbc, _, _ := newsStoryFixture(t)
	ctx := context.Background()
	legacy := addTestWatch(t, s, "legacy", core.WatchActive, watchTestNow)
	legacyItems := recordItems(t, s, legacy, "old")
	legacy.Mode, legacy.Section, legacy.UpdatedAt = core.WatchModeNews, "国际", watchTestNow
	if err := s.UpdateWatch(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	items := recordItems(t, s, bbc, "b1")
	batch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 2 {
		t.Fatal(batch, err)
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, core.NewsCommit{
		At:      watchTestNow,
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "国际", Title: "t"}},
		Scores:  []core.NewsItemScore{{ItemID: items[0].ID, Score: 3, Story: "n1"}, {ItemID: legacyItems[0].ID, Score: 2, Story: "n1"}},
	}); err != nil {
		t.Fatal(err)
	}
	pruned, err := s.PruneNews(ctx, watchTestNow.Add(time.Hour))
	if err != nil || pruned != 1 {
		t.Fatalf("pruned = %d, %v", pruned, err)
	}
	if again, err := s.PruneNews(ctx, watchTestNow.Add(time.Hour)); err != nil || again != 0 {
		t.Fatalf("pruning again = %d, %v", again, err)
	}
	current, err := s.LoadWatch(ctx, bbc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refound := recordItems(t, s, *current, "b1"); len(refound) != 0 {
		t.Fatalf("a pruned item still in the feed came back: %+v", refound)
	}
	if batch, err := s.ClaimNewsBatch(ctx, watchTestNow.Add(core.NewsRetention), time.Minute, 10, false); err != nil || len(batch.Items) != 0 {
		t.Fatalf("items first seen before the retention were claimed: %+v, %v", batch, err)
	}
	if !sourceExists(t, s, fmt.Sprintf("watch:%d", legacyItems[0].ID)) {
		t.Fatal("the legacy item's source must stay")
	}
	if found, err := s.SearchNewsStories(ctx, core.NewsQuery{Since: watchTestNow, Limit: 5}); err != nil || len(found) != 1 || len(found[0].Items) != 1 || found[0].Items[0].Key != "old" {
		t.Fatalf("the story keeps the legacy item: %+v, %v", found, err)
	}
	if err := s.UpdateWatch(ctx, func() core.Watch { legacy.Mode, legacy.Section = core.WatchModeDigest, ""; return legacy }()); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameNewsSection(ctx, "国际", "国际与财经", watchTestNow); err != nil {
		t.Fatal(err)
	}
	if found, err := s.SearchNewsStories(ctx, core.NewsQuery{Section: "国际与财经", Since: watchTestNow, Limit: 5}); err != nil || len(found) != 1 {
		t.Fatalf("stories move with their section's name: %+v, %v", found, err)
	}
}

func TestTrialSourcesStayOutOfBriefingsAndEachSourceIsCounted(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	trial := addNewsWatch(t, s, "trial", "国际", false)
	trial.Trial, trial.UpdatedAt = true, watchTestNow
	if err := s.UpdateWatch(ctx, trial); err != nil {
		t.Fatal(err)
	}
	record := func(w core.Watch, keys ...string) map[string]int64 {
		var items []core.WatchItem
		for _, key := range keys {
			items = append(items, core.WatchItem{WatchID: w.ID, Key: key, Title: key, URL: "https://" + w.Name + ".test/" + key, Status: core.WatchItemPending, SeenAt: watchTestNow})
		}
		added, err := s.RecordWatchCheck(ctx, w, core.WatchCheck{At: watchTestNow, NextCheckAt: watchTestNow.Add(time.Hour), Status: w.Status, Items: items})
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]int64{}
		for _, item := range added {
			ids[item.Key] = item.ID
		}
		return ids
	}
	ids := record(bbc, "b1", "b2")
	for k, v := range record(ft, "f1") {
		ids[k] = v
	}
	for k, v := range record(trial, "t1", "t2") {
		ids[k] = v
	}
	batch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 5 {
		t.Fatalf("batch = %+v, %v", batch, err)
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, core.NewsCommit{
		At:      watchTestNow,
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "国际", Title: "停火谈判"}, {Ref: "n2", Section: "国际", Title: "只有试用源报的事"}},
		Scores: []core.NewsItemScore{
			{ItemID: ids["b1"], Score: 5, Story: "n1"}, {ItemID: ids["b2"], Score: 1}, {ItemID: ids["f1"], Score: 4, Story: "n1"},
			{ItemID: ids["t1"], Score: 5, Story: "n1"}, {ItemID: ids["t2"], Score: 4, Story: "n2"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	news, err := s.BriefingNewsStories(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(news.Stories) != 1 || news.Stories[0].Title != "停火谈判" || len(news.Stories[0].Items) != 2 || len(news.IDs) != 5 {
		t.Fatalf("briefing news = %+v", news)
	}
	for _, item := range news.Stories[0].Items {
		if item.Source == "trial" {
			t.Fatalf("a trial item was offered: %+v", item)
		}
	}

	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-03", watchTestNow); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_b", Input: "[briefing 2026-10-03]"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, "run_b", core.RunStatusSucceeded, "1. **停火谈判**：开始了。[BBC](https://bbc.test/b1)", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoutineRun(ctx, "briefing", "2026-10-03", "thread_b", "run_b"); err != nil {
		t.Fatal(err)
	}
	stats, err := s.NewsSourceStats(ctx, watchTestNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, st := range stats {
		got = append(got, fmt.Sprintf("%s trial=%t items=%d scored=%d sum=%d high=%d shared=%d picked=%d", st.Watch.Name, st.Watch.Trial, st.Items, st.Scored, st.ScoreSum, st.High, st.Shared, st.Picked))
	}
	want := []string{
		"bbc trial=false items=2 scored=2 sum=6 high=1 shared=1 picked=1",
		"ft trial=false items=1 scored=1 sum=4 high=1 shared=1 picked=0",
		"daily trial=false items=0 scored=0 sum=0 high=0 shared=0 picked=0",
		"trial trial=true items=2 scored=2 sum=9 high=2 shared=1 picked=0",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("stats:\n%s", strings.Join(got, "\n"))
	}
	ft.Status, ft.UpdatedAt = core.WatchPaused, watchTestNow
	if err := s.UpdateWatch(ctx, ft); err != nil {
		t.Fatal(err)
	}
	if stats, err := s.NewsSourceStats(ctx, watchTestNow.Add(-time.Hour)); err != nil || len(stats) != 3 {
		t.Fatalf("paused sources are not counted: %d, %v", len(stats), err)
	}
}

func TestStoriesATrialSourceStartedShowInASectionTheBriefingTakes(t *testing.T) {
	s, bbc, _, _ := newsStoryFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "热榜", Position: 3, UpdatedAt: watchTestNow}); err != nil {
		t.Fatal(err)
	}
	zhihu := addNewsWatch(t, s, "zhihu", "热榜", false)
	zhihu.Trial, zhihu.UpdatedAt = true, watchTestNow
	if err := s.UpdateWatch(ctx, zhihu); err != nil {
		t.Fatal(err)
	}
	quake := recordItems(t, s, zhihu, "z1")
	reports := recordItems(t, s, bbc, "b1")
	batch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 2 {
		t.Fatalf("batch = %+v, %v", batch, err)
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, core.NewsCommit{
		At:      watchTestNow,
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "热榜", Title: "地震"}},
		Scores:  []core.NewsItemScore{{ItemID: quake[0].ID, Score: 5, Story: "n1"}, {ItemID: reports[0].ID, Score: 4, Story: "n1"}},
	}); err != nil {
		t.Fatal(err)
	}
	shown := func() []core.NewsStory {
		t.Helper()
		news, err := s.BriefingNewsStories(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		return news.Stories
	}
	if got := shown(); len(got) != 1 || got[0].Section != "国际" || len(got[0].Items) != 1 || got[0].Items[0].Key != "b1" {
		t.Fatalf("a story in a section of trial sources only = %+v", got)
	}
	zhihu.Trial = false
	if err := s.UpdateWatch(ctx, zhihu); err != nil {
		t.Fatal(err)
	}
	if got := shown(); len(got) != 1 || got[0].Section != "热榜" || len(got[0].Items) != 2 {
		t.Fatalf("after the trial source was taken in = %+v", got)
	}
	for _, w := range []core.Watch{zhihu, bbc} {
		paused := w.Pause(watchTestNow)
		if err := s.UpdateWatch(ctx, paused); err != nil {
			t.Fatal(err)
		}
	}
	if got := shown(); len(got) != 0 {
		t.Fatalf("stories of paused sources were offered: %+v", got)
	}
}
