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
	if n, err := s.FailedNewsItems(ctx, tomorrow); err != nil || n != 1 {
		t.Fatalf("failed = %d, %v", n, err)
	}
	if news, err := s.BriefingNewsStories(ctx, 10); err != nil || len(news.Stories) != 0 || len(news.IDs) != 1 {
		t.Fatalf("a failed item is dropped without a story: %+v, %v", news, err)
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
