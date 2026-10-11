package wire

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInterruptedRunPushesItsPendingAction(t *testing.T) {
	provider := &fakeOpenAI{replies: []string{sseChunks(recallCallStream, toolCallsFinish)}}
	server := httptest.NewServer(provider)
	defer server.Close()
	harness, extra := newPushHarness(t, time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC))
	cfg := writeTestConfig(t, server.URL, "approval:\n  require:\n    - recall\n"+briefingOff+extra)
	c := harness.open(t, cfg)
	defer func() { _ = c.Close() }()
	registerPushToken(t, c, "tok_phone")

	ctx := context.Background()
	thread, err := c.threads.CreateThread(ctx, "approval")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := c.runs.CreateRun(ctx, thread.ID, "", "find probe")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	waitRunStatus(t, c, run.ID, "interrupted")
	actions, err := c.pendingAction.List(ctx, 10)
	if err != nil || len(actions) != 1 {
		t.Fatalf("pending actions = %+v, %v", actions, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(harness.fcm.sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	sent := harness.fcm.sent()
	if len(sent) != 1 {
		t.Fatalf("fcm messages = %v, want 1", sent)
	}
	if _, shown := sent[0]["notification"]; shown {
		t.Fatalf("pending action push carries a system notification: %v", sent[0])
	}
	data, _ := sent[0]["data"].(map[string]any)
	if data["type"] != "pending_action" || data["action_id"] != actions[0].ActionID || data["thread_id"] != thread.ID || data["run_id"] != run.ID {
		t.Fatalf("fcm data = %v, want action %s thread %s run %s", data, actions[0].ActionID, thread.ID, run.ID)
	}
}
