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

func addNewsWatch(t *testing.T, s *Store, name, section string, issue bool) core.Watch {
	t.Helper()
	w, err := s.AddWatch(context.Background(), core.Watch{
		Name: name, Kind: core.WatchRSS, Target: "https://example.com/" + name, Mode: core.WatchModeNews, Section: section, Issue: issue,
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: watchTestNow, CreatedAt: watchTestNow,
	})
	if err != nil {
		t.Fatalf("add news watch: %v", err)
	}
	return w
}

// recordItems records a check of w that finds the given items, each pending
// and published at its index in hours after watchTestNow.
func recordItems(t *testing.T, s *Store, w core.Watch, keys ...string) []core.WatchItem {
	t.Helper()
	var items []core.WatchItem
	for i, key := range keys {
		items = append(items, core.WatchItem{WatchID: w.ID, Key: key, Title: w.Name + " " + key, Status: core.WatchItemPending,
			SeenAt: watchTestNow, PublishedAt: watchTestNow.Add(time.Duration(i) * time.Hour)})
	}
	added, err := s.RecordWatchCheck(context.Background(), w, core.WatchCheck{At: watchTestNow, NextCheckAt: watchTestNow.Add(time.Hour), Status: w.Status, Items: items})
	if err != nil {
		t.Fatalf("record items of %s: %v", w.Name, err)
	}
	return added
}

func TestNewsSectionsAreSavedRenamedWithTheirWatchesAndKeptWhileInUse(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "AI", Position: 2, Preference: "1. 模型", UpdatedAt: watchTestNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "国内要闻", Position: 1, UpdatedAt: watchTestNow}); err != nil {
		t.Fatal(err)
	}
	later := watchTestNow.Add(time.Hour)
	saved, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "AI", Position: 3, Preference: "1. 模型\n2. agent", UpdatedAt: later})
	if err != nil || saved.Position != 3 || saved.Preference != "1. 模型\n2. agent" || !saved.CreatedAt.Equal(watchTestNow) || !saved.UpdatedAt.Equal(later) {
		t.Fatalf("updated section = %+v, %v", saved, err)
	}
	sections, err := s.ListNewsSections(ctx)
	if err != nil || len(sections) != 2 || sections[0].Name != "国内要闻" || sections[1].Name != "AI" {
		t.Fatalf("sections = %+v, %v", sections, err)
	}

	if _, err := s.AddWatch(ctx, core.Watch{Name: "x", Kind: core.WatchRSS, Target: "https://x.test", Mode: core.WatchModeNews, Section: "体育",
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: watchTestNow, CreatedAt: watchTestNow}); !errors.Is(err, core.ErrNewsSectionNotFound) {
		t.Fatalf("watch in a missing section err = %v", err)
	}
	if _, err := s.AddWatch(ctx, core.Watch{Name: "x", Kind: core.WatchRSS, Target: "https://x.test", Mode: core.WatchModeNews,
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: watchTestNow, CreatedAt: watchTestNow}); err == nil {
		t.Fatal("a news watch needs a section")
	}
	if _, err := s.AddWatch(ctx, core.Watch{Name: "x", Kind: core.WatchRSS, Target: "https://x.test", Mode: core.WatchModeDigest, Section: "AI",
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: watchTestNow, CreatedAt: watchTestNow}); err == nil {
		t.Fatal("only news watches have a section")
	}
	w := addNewsWatch(t, s, "ai225", "AI", true)
	if err := s.RenameNewsSection(ctx, "AI", "AI 与科技前沿", later); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.LoadWatch(ctx, w.ID)
	if err != nil || loaded.Section != "AI 与科技前沿" || !loaded.Issue || loaded.Mode != core.WatchModeNews {
		t.Fatalf("watch after rename = %+v, %v", loaded, err)
	}
	if err := s.RenameNewsSection(ctx, "AI 与科技前沿", "国内要闻", later); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("rename onto another section err = %v", err)
	}
	if err := s.RenameNewsSection(ctx, "体育", "运动", later); !errors.Is(err, core.ErrNewsSectionNotFound) {
		t.Fatalf("rename missing err = %v", err)
	}
	if err := s.DeleteNewsSection(ctx, "AI 与科技前沿"); !errors.Is(err, core.ErrNewsSectionInUse) || !strings.Contains(err.Error(), "ai225") {
		t.Fatalf("delete in use err = %v", err)
	}
	loaded.Mode, loaded.Section, loaded.Issue, loaded.UpdatedAt = core.WatchModeDigest, "", false, later
	if err := s.UpdateWatch(ctx, *loaded); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNewsSection(ctx, "AI 与科技前沿"); err != nil {
		t.Fatalf("delete an empty section: %v", err)
	}
	if err := s.DeleteNewsSection(ctx, "AI 与科技前沿"); !errors.Is(err, core.ErrNewsSectionNotFound) {
		t.Fatalf("delete twice err = %v", err)
	}
}

func TestBriefingItemsTakeEachWatchInTurn(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	busy := addTestWatch(t, s, "busy", core.WatchActive, watchTestNow)
	quiet := addTestWatch(t, s, "quiet", core.WatchActive, watchTestNow)
	var keys []string
	for i := range 6 {
		keys = append(keys, fmt.Sprintf("b%d", i))
	}
	recordItems(t, s, busy, keys...)
	recordItems(t, s, quiet, "q0")
	if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "AI", UpdatedAt: watchTestNow}); err != nil {
		t.Fatal(err)
	}
	recordItems(t, s, addNewsWatch(t, s, "news", "AI", false), "n0")

	got, err := s.PendingBriefingItems(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, item := range got.Items {
		listed = append(listed, item.Key)
	}
	if strings.Join(listed, ",") != "b0,q0,b1" {
		t.Fatalf("listed = %v", listed)
	}
	if got.Counts[busy.ID] != 6 || got.Counts[quiet.ID] != 1 || len(got.Counts) != 2 {
		t.Fatalf("counts = %v", got.Counts)
	}
}

func TestBriefingItemsLeaveOutPausedWatches(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	blog := addTestWatch(t, s, "blog", core.WatchActive, watchTestNow)
	recordItems(t, s, blog, "b0")
	wire := addTestWatch(t, s, "wire", core.WatchActive, watchTestNow)
	wireItems := recordItems(t, s, wire, "w0", "w1")
	loaded, err := s.LoadWatch(ctx, wire.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateWatch(ctx, loaded.Pause(watchTestNow)); err != nil {
		t.Fatal(err)
	}

	got, err := s.PendingBriefingItems(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Key != "b0" || len(got.Counts) != 1 || got.Counts[blog.ID] != 1 {
		t.Fatalf("items = %+v, counts = %v", got.Items, got.Counts)
	}
	if !slices.Equal(got.Paused, []int64{wireItems[0].ID, wireItems[1].ID}) {
		t.Fatalf("paused = %v, want the ids of %v", got.Paused, wireItems)
	}
}

func TestNewsItemsStayOutOfWatchChangesAndMemory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "国际", UpdatedAt: watchTestNow}); err != nil {
		t.Fatal(err)
	}
	bbc := addNewsWatch(t, s, "bbc", "国际", false)
	paused := addNewsWatch(t, s, "paused", "国际", false)
	bbcItems := recordItems(t, s, bbc, "b0", "b1")
	pausedItems := recordItems(t, s, paused, "p0")
	paused.Status, paused.UpdatedAt = core.WatchPaused, watchTestNow
	if err := s.UpdateWatch(ctx, paused); err != nil {
		t.Fatal(err)
	}
	digest := addTestWatch(t, s, "digest", core.WatchActive, watchTestNow)
	digestItems := recordItems(t, s, digest, "d0")

	pending, err := s.PendingBriefingItems(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Items) != 1 || pending.Items[0].Key != "d0" || len(pending.Counts) != 1 {
		t.Fatalf("watch changes = %v, counts = %v", pending.Items, pending.Counts)
	}
	for _, item := range append(bbcItems, pausedItems...) {
		if sourceExists(t, s, fmt.Sprintf("watch:%d", item.ID)) {
			t.Fatalf("news item %s became a memory source", item.Key)
		}
	}
	if !sourceExists(t, s, fmt.Sprintf("watch:%d", digestItems[0].ID)) {
		t.Fatal("a digest item is a memory source")
	}
}

func sourceExists(t *testing.T, s *Store, id string) bool {
	t.Helper()
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM memory_sources WHERE id = ?)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestTriggerSourcesAreNewsWatchesThatAreNeverDue(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveNewsSection(ctx, core.NewsSection{Name: "热榜", UpdatedAt: watchTestNow}); err != nil {
		t.Fatal(err)
	}
	source := core.Watch{Name: "知乎热榜", Kind: core.WatchTrigger, Target: "hot-lists/zhihu", Mode: core.WatchModeNews, Section: "热榜",
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: watchTestNow, CreatedAt: watchTestNow}
	if _, err := s.AddWatch(ctx, source); err != nil {
		t.Fatal(err)
	}
	feed := addNewsWatch(t, s, "bbc", "热榜", false)
	due, err := s.ListDueWatches(ctx, watchTestNow, 10)
	if err != nil || len(due) != 1 || due[0].ID != feed.ID {
		t.Fatalf("due = %+v, %v", due, err)
	}
	source.Mode, source.Section = core.WatchModeDigest, ""
	if _, err := s.AddWatch(ctx, source); err == nil || !strings.Contains(err.Error(), "trigger source is a news watch") {
		t.Fatalf("a digest trigger source err = %v", err)
	}
}
