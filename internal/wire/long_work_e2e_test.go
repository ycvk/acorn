package wire

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// The owner hands over work that outlasts a run. The agent sets a goal,
// starts a download that keeps running as a job and sets a follow-up on the
// file. Once the file is ready the agent wakes in the conversation; the goal
// then continues it until the agent reports it done, and the owner gets one
// push for the goal's end.
func TestLongWorkContinuesUntilTheGoalIsDone(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken, Supervisor: workstation.NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	h, extra := newPushHarness(t, time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC))
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+fmt.Sprintf("workstation:\n  url: %s\n  token: %s\n", wsServer.URL, fileTestToken), briefingOff)
	c := h.open(t, cfg)
	defer c.Close()
	registerPushInstallation(t, c, "fid_phone")
	thread, err := c.threads.CreateThread(ctx, "下书")
	if err != nil {
		t.Fatal(err)
	}

	provider.add(
		toolCallChunk("load", "tool_search", map[string]string{"query": "select:shell,follow_up"}),
		toolCallChunk("goal", "goal", map[string]any{"action": "start", "objective": "把书下好并整理成笔记", "max_runs": 3}),
		toolCallChunk("download", "shell", map[string]any{"command": "sleep 1; printf 第一章 > book.txt", "wait_seconds": 0}),
		toolCallChunk("wait", "follow_up", map[string]string{"task": "书下好了就整理", "path": "book.txt"}),
		textReply("开始下载了，下好我接着整理。"),
	)
	if _, err := startOwnerRun(ctx, c, thread.ID, "把这本书下好整理成笔记"); err != nil {
		t.Fatal(err)
	}
	latest := waitNewRun(t, c, thread.ID, "").RunID
	waiting, err := c.store.WaitingFollowUps(ctx, thread.ID)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("follow-ups = %+v, %v", waiting, err)
	}

	// Nothing wakes while the download runs; the goal waits for the follow-up.
	calls := provider.requestCount()
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.requestCount() != calls {
		t.Fatal("a run started before the file was ready")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(home, "book.txt")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the download job never wrote the file")
		}
		time.Sleep(50 * time.Millisecond)
	}
	provider.add(textReply("书下好了，接下来整理。"))
	for range 2 {
		if err := c.wake.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	latest = waitNewRun(t, c, thread.ID, latest).RunID
	woken := requestText(provider.request(calls))
	if !strings.Contains(woken, "[follow_up #1, set 2026-11-03 08:00] 书下好了就整理\nWhat happened: "+filepath.Join(home, "book.txt")+" is ready: 9 bytes, unchanged since the previous check") {
		t.Fatalf("follow-up run input:\n%s", woken)
	}
	if !strings.Contains(woken, "## Goal of this conversation\n- #1 [0 of 3 runs taken] 把书下好并整理成笔记") {
		t.Fatalf("the follow-up run's presence lacks the goal:\n%s", woken)
	}

	// The goal continues the conversation after the follow-up's run.
	calls = provider.requestCount()
	provider.add(
		toolCallChunk("done", "goal", map[string]string{"action": "done", "result": "书已下好，整理成了笔记《第一章》"}),
		textReply("整理好了。"),
	)
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, latest)
	if text := requestText(provider.request(calls)); !strings.Contains(text, "[goal #1, run 1 of 3] Objective: 把书下好并整理成笔记") {
		t.Fatalf("goal run input:\n%s", text)
	}
	for range 2 {
		if err := c.wake.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	sent := h.fcm.sent()
	if len(sent) != 1 {
		t.Fatalf("pushes = %v, want the goal's end once", sent)
	}
	notification, _ := sent[0]["notification"].(map[string]any)
	data, _ := sent[0]["data"].(map[string]any)
	if notification["title"] != "目标完成" || notification["body"] != "书已下好，整理成了笔记《第一章》" || data["thread_id"] != thread.ID {
		t.Fatalf("push = %v", sent[0])
	}
	if provider.requestCount() != calls+2 {
		t.Fatalf("model requests = %d, want no run after the goal ended", provider.requestCount())
	}
	if wakes, err := c.store.CountWakesSince(ctx, time.Time{}); err != nil || wakes != 2 {
		t.Fatalf("wakes = %d, %v; want the follow-up and the goal run", wakes, err)
	}
	if _, err := c.store.ActiveGoal(ctx, thread.ID); err != core.ErrGoalNotFound {
		t.Fatalf("active goal after done: %v", err)
	}
}
