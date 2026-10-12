package store

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestNotificationsKeepTheirActionAcrossTheV2Upgrade(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	if _, err := raw.Exec(readLayout(t, 1) + `PRAGMA user_version = 1;
		INSERT INTO notifications(title, body, thread_id, run_id, status, send_after, created_at)
		VALUES('提醒', '交周报', 'thread_1', 'run_1', 'queued', '2026-10-11T00:00:00.000000000Z', '2026-10-11T00:00:00.000000000Z');`); err != nil {
		t.Fatalf("build v1: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	if _, err := store.QueueNotification(ctx, core.Notification{Title: "需要你批准", Body: "发送邮件", ThreadID: "thread_2", ActionID: "action_1", SendAfter: at, CreatedAt: at}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	due, err := store.ListDueNotifications(ctx, at)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 2 || due[0].Title != "提醒" || due[0].ActionID != "" || due[1].ActionID != "action_1" {
		t.Fatalf("due = %+v", due)
	}
}

func TestNotificationsKeepTheirPageAcrossTheV3Upgrade(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	if _, err := raw.Exec(readLayout(t, 2) + `PRAGMA user_version = 2;
		INSERT INTO notifications(title, body, thread_id, run_id, action_id, status, send_after, created_at)
		VALUES('需要你批准', '发送邮件', 'thread_1', 'run_1', 'action_1', 'queued', '2026-10-11T00:00:00.000000000Z', '2026-10-11T00:00:00.000000000Z');`); err != nil {
		t.Fatalf("build v2: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	if _, err := store.QueueNotification(ctx, core.Notification{Title: "需要登录", Body: "在浏览器里登录 GitHub", ThreadID: "thread_2", BrowserURL: "https://github.com/login", SendAfter: at, CreatedAt: at}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	due, err := store.ListDueNotifications(ctx, at)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 2 || due[0].ActionID != "action_1" || due[0].BrowserURL != "" || due[1].BrowserURL != "https://github.com/login" {
		t.Fatalf("due = %+v", due)
	}
}
