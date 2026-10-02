package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ycvk/acorn/internal/core"
)

func TestMemoryItemLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	expires := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)
	if _, err := s.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemorySaid, Content: "x", Status: core.MemoryActive}); err == nil {
		t.Fatal("an item without created_at must be rejected")
	}
	added, err := s.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemorySaid, Content: "我喜欢早上看新闻", Status: core.MemoryActive, SessionID: "thread_1", ExpiresAt: expires, CreatedAt: created})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if added.ID == 0 {
		t.Fatalf("added = %+v", added)
	}
	loaded, err := s.LoadMemoryItem(ctx, added.ID)
	if err != nil || !loaded.ExpiresAt.Equal(expires) || !loaded.WakeAt.IsZero() || loaded.SessionID != "thread_1" ||
		!loaded.CreatedAt.Equal(created) || !loaded.UpdatedAt.Equal(created) {
		t.Fatalf("load = %+v err=%v", loaded, err)
	}
	loaded.Status = core.MemoryResting
	loaded.UpdatedAt = time.Time{}
	if err := s.UpdateMemoryItem(ctx, *loaded); err == nil {
		t.Fatal("an update without updated_at must be rejected")
	}
	loaded.UpdatedAt = created.Add(time.Hour)
	if err := s.UpdateMemoryItem(ctx, *loaded); err != nil {
		t.Fatalf("update: %v", err)
	}
	resting, err := s.ListMemoryItems(ctx, []core.MemoryStatus{core.MemoryResting})
	if err != nil || len(resting) != 1 || resting[0].ID != added.ID || !resting[0].UpdatedAt.Equal(created.Add(time.Hour)) {
		t.Fatalf("resting = %+v err=%v", resting, err)
	}
	if err := s.UpdateMemoryItem(ctx, core.MemoryItem{ID: 999, Content: "x", Status: core.MemoryActive, UpdatedAt: created}); !errors.Is(err, core.ErrMemoryItemNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if _, err := s.LoadMemoryItem(ctx, 999); !errors.Is(err, core.ErrMemoryItemNotFound) {
		t.Fatalf("load missing: %v", err)
	}
}

func TestClaimDueCommitmentOnlyOnce(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	due, _ := s.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemoryCommitment, Content: "提醒看 X", Status: core.MemoryActive, WakeAt: now.Add(-time.Minute), CreatedAt: now})
	later, _ := s.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemoryCommitment, Content: "later", Status: core.MemoryActive, WakeAt: now.Add(time.Hour), CreatedAt: now})

	list, err := s.ListDueCommitments(ctx, now)
	if err != nil || len(list) != 1 || list[0].ID != due.ID {
		t.Fatalf("due = %+v err=%v", list, err)
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
	)
	for range 8 {
		wg.Go(func() {
			err := s.ClaimDueCommitment(ctx, due.ID, now)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if !errors.Is(err, core.ErrMemoryItemNotDue) {
				t.Errorf("claim: %v", err)
			}
		})
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("claims succeeded %d times, want 1", success)
	}
	if err := s.ClaimDueCommitment(ctx, later.ID, now); !errors.Is(err, core.ErrMemoryItemNotDue) {
		t.Fatalf("claim not-yet-due: %v", err)
	}
	woken, _ := s.LoadMemoryItem(ctx, due.ID)
	if woken.Status != core.MemoryWoken {
		t.Fatalf("status = %s", woken.Status)
	}
}

func TestSearchExperienceCoversRunsAndMemory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_1", Input: "三天后提醒我看那篇论文"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.FinishRun(ctx, "run_1", core.RunStatusSucceeded, "好的,已经约好了 deadline", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if _, err := s.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemoryThought, Content: "owner 最近在读论文", Status: core.MemoryActive, SourceRunID: "run_1", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add: %v", err)
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{query: "提醒我看", want: 1},
		{query: "deadline", want: 1},
		{query: "论文", want: 2}, // two runes: LIKE fallback over both sources
		{query: `"unbalanced`, want: 0},
	} {
		hits, err := s.SearchExperience(ctx, tc.query, 10)
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		if len(hits) != tc.want {
			t.Fatalf("search %q = %+v, want %d hits", tc.query, hits, tc.want)
		}
		for _, h := range hits {
			if h.Snippet == "" || h.CreatedAt.IsZero() || (h.Source != "run" && h.Source != "memory") {
				t.Fatalf("hit = %+v", h)
			}
		}
	}
	if _, err := s.SearchExperience(ctx, " ", 10); err == nil {
		t.Fatal("empty query must fail")
	}
}

func TestRunsFTSBackfillsExistingRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_old", Input: "an older conversation"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Simulate a database from before runs_fts existed.
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	for _, stmt := range []string{
		`DROP TRIGGER runs_fts_ai`, `DROP TRIGGER runs_fts_au`, `DROP TABLE runs_fts`,
		`DELETE FROM schema_migrations WHERE version = 'v4_runs_fts_backfill'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	_ = db.Close()

	s, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s.Close() }()
	hits, err := s.SearchExperience(context.Background(), "older", 5)
	if err != nil || len(hits) != 1 || hits[0].RunID != "run_old" {
		t.Fatalf("hits = %+v err=%v", hits, err)
	}
}

func TestPushTokensFollowDeviceRevocation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"dev_a", "dev_b"} {
		if err := s.SaveDevice(ctx, &core.Device{DeviceID: id, Name: id, Platform: "android", TokenHash: "hash_" + id, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("save device: %v", err)
		}
		if err := s.SetPushToken(ctx, id, "fcm_"+id); err != nil {
			t.Fatalf("set token: %v", err)
		}
	}
	if err := s.SetPushToken(ctx, "dev_a", "fcm_a2"); err != nil {
		t.Fatalf("replace token: %v", err)
	}
	if err := s.RevokeDevice(ctx, "dev_b", time.Now()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	tokens, err := s.ListPushTokens(ctx)
	if err != nil || len(tokens) != 1 || tokens[0].Token != "fcm_a2" {
		t.Fatalf("tokens = %+v err=%v", tokens, err)
	}
}

func TestPushTokenBelongsToLatestDevice(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	// The same phone paired twice: the app keeps one FCM token across pairings.
	for _, id := range []string{"dev_old", "dev_new"} {
		if err := s.SaveDevice(ctx, &core.Device{DeviceID: id, Name: id, Platform: "android", TokenHash: "hash_" + id, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("save device: %v", err)
		}
		if err := s.SetPushToken(ctx, id, "fcm_phone"); err != nil {
			t.Fatalf("set token: %v", err)
		}
	}
	tokens, err := s.ListPushTokens(ctx)
	if err != nil || len(tokens) != 1 || tokens[0].DeviceID != "dev_new" {
		t.Fatalf("tokens = %+v err=%v, want the token once, on the latest device", tokens, err)
	}
}

func TestNotificationQueueAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	soon, err := s.QueueNotification(ctx, core.Notification{Title: "t", Body: "b", ThreadID: "thread_1", SendAfter: now.Add(-time.Second), CreatedAt: now})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if _, err := s.QueueNotification(ctx, core.Notification{Title: "t", Body: "b", SendAfter: now.Add(time.Hour), CreatedAt: now}); err != nil {
		t.Fatalf("queue later: %v", err)
	}
	due, err := s.ListDueNotifications(ctx, now)
	if err != nil || len(due) != 1 || due[0].ID != soon.ID || due[0].ThreadID != "thread_1" {
		t.Fatalf("due = %+v err=%v", due, err)
	}
	if err := s.FinishNotification(ctx, soon.ID, core.NotificationSent, "", now); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if due, _ := s.ListDueNotifications(ctx, now); len(due) != 0 {
		t.Fatalf("sent notification still due: %+v", due)
	}
	if n, err := s.CountNotificationsSince(ctx, now.Add(-time.Minute)); err != nil || n != 2 {
		t.Fatalf("count = %d err=%v", n, err)
	}
	if err := s.FinishNotification(ctx, soon.ID, core.NotificationQueued, "", now); err == nil {
		t.Fatal("finishing as queued must fail")
	}
}

func TestContextSnapshotIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for range 2 {
		if err := s.SaveContextSnapshot(ctx, "h1", "<presence/>"); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM context_snapshots`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("snapshots = %d err=%v", n, err)
	}
}

func TestCountWakesSince(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_w", Input: "wake"}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Second)
	for range 2 {
		if _, err := s.AppendEvent(ctx, "run_w", core.EventWakeFired, map[string]any{"memory_id": 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AppendEvent(ctx, "run_w", "run.started", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountWakesSince(ctx, before); err != nil || n != 2 {
		t.Fatalf("wakes = %d err=%v", n, err)
	}
	if n, _ := s.CountWakesSince(ctx, time.Now().UTC().Add(time.Hour)); n != 0 {
		t.Fatalf("future window counted %d", n)
	}
}
