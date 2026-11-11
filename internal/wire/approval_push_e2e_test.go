package wire

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/settings"
)

// The call here is far larger than an FCM message may be: the push announces
// the action without carrying what it asks.
func TestInterruptedRunPushesItsPendingAction(t *testing.T) {
	largeCall := strings.Replace(recallCallStream, "probe", strings.Repeat("probe ", 1200), 1)
	provider := &fakeOpenAI{replies: []string{sseChunks(largeCall, toolCallsFinish)}}
	server := httptest.NewServer(provider)
	defer server.Close()
	harness, extra := newPushHarness(t, time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC))
	cfg := writeTestConfig(t, server.URL, extra, ownerSettings{settings.KeyApprovalRequire: `["recall"]`}, briefingOff)
	c := harness.open(t, cfg)
	defer func() { _ = c.Close() }()
	registerPushInstallation(t, c, "fid_phone")

	ctx := context.Background()
	thread, err := c.threads.CreateThread(ctx, "approval")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := startOwnerRun(ctx, c, thread.ID, "find probe")
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

// The owner asked for something just now and the run stops for their
// approval inside quiet hours: the push goes out at once.
func TestApprovalTheOwnerWaitsForPushesInsideQuietHours(t *testing.T) {
	provider := &fakeOpenAI{replies: []string{sseChunks(recallCallStream, toolCallsFinish)}}
	server := httptest.NewServer(provider)
	defer server.Close()
	harness, extra := newPushHarness(t, time.Date(2026, 10, 5, 23, 30, 0, 0, time.UTC))
	cfg := writeTestConfig(t, server.URL, extra, ownerSettings{settings.KeyApprovalRequire: `["recall"]`}, briefingOff)
	c := harness.open(t, cfg)
	defer func() { _ = c.Close() }()
	registerPushInstallation(t, c, "fid_phone")

	ctx := context.Background()
	thread, err := c.threads.CreateThread(ctx, "approval at night")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := startOwnerRun(ctx, c, thread.ID, "find probe")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	waitRunStatus(t, c, run.ID, "interrupted")
	deadline := time.Now().Add(5 * time.Second)
	for len(harness.fcm.sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	sent := harness.fcm.sent()
	if len(sent) != 1 {
		t.Fatalf("fcm messages inside quiet hours = %v, want the approval", sent)
	}
	if data, _ := sent[0]["data"].(map[string]any); data["type"] != "pending_action" || data["run_id"] != run.ID {
		t.Fatalf("fcm data = %v, want the pending action of run %s", data, run.ID)
	}
}
