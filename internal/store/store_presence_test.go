package store

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ycvk/acorn/internal/core"
)

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
