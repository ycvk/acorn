package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var watchTestNow = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

func addTestWatch(t *testing.T, s *Store, name string, status core.WatchStatus, next time.Time) core.Watch {
	t.Helper()
	w, err := s.AddWatch(context.Background(), core.Watch{
		Name: name, Kind: core.WatchRSS, Target: "https://example.com/feed", Mode: core.WatchModeDigest,
		Interval: time.Hour, Status: status, SessionID: "thread_1", NextCheckAt: next, CreatedAt: watchTestNow,
	})
	if err != nil {
		t.Fatalf("add watch: %v", err)
	}
	return w
}

func TestWatchLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.AddWatch(ctx, core.Watch{Name: "x"}); err == nil {
		t.Fatal("an incomplete watch must be rejected")
	}
	w := addTestWatch(t, s, "feed", core.WatchActive, watchTestNow)
	loaded, err := s.LoadWatch(ctx, w.ID)
	if err != nil || loaded.Interval != time.Hour || loaded.Mode != core.WatchModeDigest || !loaded.LastCheckedAt.IsZero() {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}
	loaded.Status, loaded.Failures, loaded.LastError, loaded.Snapshot = core.WatchFailing, 2, "boom", "¥2999"
	loaded.LastCheckedAt, loaded.UpdatedAt = watchTestNow, watchTestNow
	if err := s.UpdateWatch(ctx, *loaded); err != nil {
		t.Fatalf("update: %v", err)
	}
	all, err := s.ListWatches(ctx)
	if err != nil || len(all) != 1 || all[0].Status != core.WatchFailing || all[0].Snapshot != "¥2999" || !all[0].LastCheckedAt.Equal(watchTestNow) {
		t.Fatalf("list = %+v, %v", all, err)
	}
	if _, err := s.LoadWatch(ctx, 99); !errors.Is(err, core.ErrWatchNotFound) {
		t.Fatalf("load missing err = %v", err)
	}
}

func TestDueWatchesAndClaim(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	due := addTestWatch(t, s, "due", core.WatchActive, watchTestNow.Add(-time.Minute))
	addTestWatch(t, s, "later", core.WatchActive, watchTestNow.Add(time.Hour))
	addTestWatch(t, s, "paused", core.WatchPaused, watchTestNow.Add(-time.Hour))
	failing := addTestWatch(t, s, "failing", core.WatchFailing, watchTestNow.Add(-2*time.Minute))
	list, err := s.ListDueWatches(ctx, watchTestNow, 10)
	if err != nil || len(list) != 2 || list[0].ID != failing.ID || list[1].ID != due.ID {
		t.Fatalf("due = %+v, %v", list, err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.ClaimDueWatch(ctx, due.ID, watchTestNow, 10*time.Minute)
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, core.ErrWatchNotDue):
			t.Fatalf("claim err = %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("claims won = %d, want 1", won)
	}
	if list, _ := s.ListDueWatches(ctx, watchTestNow, 10); len(list) != 1 {
		t.Fatalf("claimed watch still due: %+v", list)
	}
}

func TestWatchCheckKeepsEditsMadeDuringIt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	w := addTestWatch(t, s, "feed", core.WatchActive, watchTestNow)
	if err := s.ClaimDueWatch(ctx, w.ID, watchTestNow, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	edited := w
	edited.Name, edited.UpdatedAt = "renamed", watchTestNow
	if err := s.UpdateWatch(ctx, edited); err != nil {
		t.Fatal(err)
	}
	item := core.WatchItem{WatchID: w.ID, Key: "a", Title: "a", Status: core.WatchItemBaseline, SeenAt: watchTestNow}
	checked := core.WatchCheck{At: watchTestNow, NextCheckAt: watchTestNow.Add(time.Hour), Status: core.WatchActive, Snapshot: "¥1", Items: []core.WatchItem{item}}
	if _, err := s.RecordWatchCheck(ctx, w, checked); err != nil {
		t.Fatalf("a rename does not touch the check: %v", err)
	}
	loaded, err := s.LoadWatch(ctx, w.ID)
	if err != nil || loaded.Name != "renamed" || loaded.Snapshot != "¥1" || !loaded.LastCheckedAt.Equal(watchTestNow) {
		t.Fatalf("after check = %+v, %v", loaded, err)
	}

	if err := s.ClaimDueWatch(ctx, w.ID, watchTestNow.Add(time.Hour), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	paused := *loaded
	paused.Status, paused.UpdatedAt = core.WatchPaused, watchTestNow.Add(time.Hour)
	if err := s.UpdateWatch(ctx, paused); err != nil {
		t.Fatal(err)
	}
	item.Key, item.Status = "b", core.WatchItemPending
	failed := core.WatchCheck{At: watchTestNow.Add(time.Hour), NextCheckAt: watchTestNow.Add(3 * time.Hour), Status: core.WatchActive, Failures: 1, LastError: "HTTP 500", Items: []core.WatchItem{item}}
	if _, err := s.RecordWatchCheck(ctx, *loaded, failed); !errors.Is(err, core.ErrWatchChanged) {
		t.Fatalf("check after a pause err = %v", err)
	}
	after, err := s.LoadWatch(ctx, w.ID)
	if err != nil || after.Status != core.WatchPaused || after.Failures != 0 || after.LastError != "" || !after.LastCheckedAt.Equal(watchTestNow) {
		t.Fatalf("paused during the check = %+v, %v", after, err)
	}
	if due, err := s.ListDueWatches(ctx, watchTestNow.Add(48*time.Hour), 10); err != nil || len(due) != 0 {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if pending, err := s.ListWatchItems(ctx, core.WatchItemPending, 10); err != nil || len(pending) != 0 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	missing := w
	missing.ID = 99
	if _, err := s.RecordWatchCheck(ctx, missing, checked); !errors.Is(err, core.ErrWatchNotFound) {
		t.Fatalf("missing watch err = %v", err)
	}
}

func TestWatchItemsDedupeAndBriefings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	w := addTestWatch(t, s, "feed", core.WatchActive, watchTestNow)
	item := func(key string) core.WatchItem {
		return core.WatchItem{WatchID: w.ID, Key: key, Title: "t " + key, Status: core.WatchItemPending, SeenAt: watchTestNow}
	}
	added, err := s.RecordWatchCheck(ctx, w, core.WatchCheck{At: watchTestNow, NextCheckAt: watchTestNow.Add(time.Hour), Status: core.WatchActive, Items: []core.WatchItem{item("a"), item("b")}})
	if err != nil || len(added) != 2 || added[0].ID == 0 {
		t.Fatalf("added = %+v, %v", added, err)
	}
	w.LastCheckedAt = watchTestNow
	added, err = s.RecordWatchCheck(ctx, w, core.WatchCheck{At: watchTestNow, NextCheckAt: watchTestNow.Add(time.Hour), Status: core.WatchActive, Items: []core.WatchItem{item("b"), item("c")}})
	if err != nil || len(added) != 1 || added[0].Key != "c" {
		t.Fatalf("second add = %+v, %v", added, err)
	}
	if err := s.MarkWatchItems(ctx, []int64{added[0].ID}, core.WatchItemBriefed, "run_9"); err != nil {
		t.Fatal(err)
	}
	pending, err := s.ListWatchItems(ctx, core.WatchItemPending, 10)
	if err != nil || len(pending) != 2 || pending[0].Key != "a" {
		t.Fatalf("pending = %+v, %v", pending, err)
	}

	if thread, err := s.LatestRoutineThread(ctx, "briefing"); err != nil || thread != "" {
		t.Fatalf("no briefing yet: %q, %v", thread, err)
	}
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-03", watchTestNow); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-03", watchTestNow); !errors.Is(err, core.ErrRoutineTaken) {
		t.Fatalf("second claim err = %v", err)
	}
	if err := s.ReleaseRoutine(ctx, "briefing", "2026-10-03"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimRoutine(ctx, "briefing", "2026-10-03", watchTestNow); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if thread, err := s.LatestRoutineThread(ctx, "briefing"); err != nil || thread != "" {
		t.Fatalf("a briefing without a run has no thread yet: %q, %v", thread, err)
	}
	if err := s.SetRoutineRun(ctx, "briefing", "2026-10-03", "thread_b", "run_b"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseRoutine(ctx, "briefing", "2026-10-03"); err != nil {
		t.Fatal(err)
	}
	if thread, err := s.LatestRoutineThread(ctx, "briefing"); err != nil || thread != "thread_b" {
		t.Fatalf("latest thread = %q, %v", thread, err)
	}
}
