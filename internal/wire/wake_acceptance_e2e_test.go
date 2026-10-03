package wire

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// fakeFCM accepts FCM HTTP v1 sends and records each message.
type fakeFCM struct {
	mu       sync.Mutex
	messages []map[string]any
}

func (f *fakeFCM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	var body struct {
		Message map[string]any `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.messages = append(f.messages, body.Message)
	f.mu.Unlock()
	_, _ = io.WriteString(w, `{"name":"projects/p/messages/1"}`)
}

func (f *fakeFCM) sent() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.messages...)
}

type pushHarness struct {
	clock *testClock
	fcm   *fakeFCM
	opts  buildOptions
}

// newPushHarness serves a fake OAuth token endpoint and FCM, and writes a
// service account file whose token_uri points at the fake.
func newPushHarness(t *testing.T, start time.Time) (*pushHarness, string) {
	t.Helper()
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fake-access","token_type":"Bearer","expires_in":3600}`)
	}))
	t.Cleanup(tokens.Close)
	fcm := &fakeFCM{}
	fcmServer := httptest.NewServer(fcm)
	t.Cleanup(fcmServer.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	account, err := json.Marshal(map[string]string{
		"project_id":   "acorn-test",
		"client_email": "acorn@acorn-test.iam.gserviceaccount.com",
		"private_key":  string(keyPEM),
		"token_uri":    tokens.URL,
	})
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}
	accountPath := filepath.Join(t.TempDir(), "fcm.json")
	if err := os.WriteFile(accountPath, account, 0o600); err != nil {
		t.Fatalf("write service account: %v", err)
	}
	clock := &testClock{now: start}
	// These tests are about commitments; the morning briefing has its own.
	extra := fmt.Sprintf("notify:\n  fcm:\n    service_account_file: %s\nbriefing:\n  at: \"\"\n", accountPath)
	return &pushHarness{
		clock: clock,
		fcm:   fcm,
		opts:  buildOptions{clock: clock.Now, fcmEndpoint: fcmServer.URL},
	}, extra
}

func (h *pushHarness) open(t *testing.T, cfg *config.Config) *Container {
	t.Helper()
	c, err := buildContainer(context.Background(), cfg, h.opts)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	return c
}

func registerPushToken(t *testing.T, c *Container, token string) {
	t.Helper()
	ctx := context.Background()
	code, err := c.DeviceAuth().CreatePairingCode(ctx, time.Minute)
	if err != nil {
		t.Fatalf("pairing code: %v", err)
	}
	paired, err := c.DeviceAuth().PairDevice(ctx, api.PairDeviceInput{PairingCode: code.Code, DeviceName: "phone", Platform: "android"})
	if err != nil {
		t.Fatalf("pair device: %v", err)
	}
	if err := c.DeviceAuth().SetPushToken(ctx, paired.Device.DeviceID, token); err != nil {
		t.Fatalf("set push token: %v", err)
	}
}

func toolCallChunk(callID, name string, args any) string {
	encoded, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	chunk, err := json.Marshal(map[string]any{
		"id": "c", "object": "chat.completion.chunk", "created": 1, "model": "fake",
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"index": 0, "id": callID, "type": "function",
				"function": map[string]any{"name": name, "arguments": string(encoded)},
			}}},
			"finish_reason": nil,
		}},
	})
	if err != nil {
		panic(err)
	}
	return sseChunks(string(chunk), toolCallsFinish)
}

func textReply(text string) string {
	chunk, err := json.Marshal(map[string]any{
		"id": "c", "object": "chat.completion.chunk", "created": 1, "model": "fake",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": nil}},
	})
	if err != nil {
		panic(err)
	}
	return sseChunks(string(chunk), stopFinish)
}

func requestMessages(request map[string]any) []map[string]any {
	raw, _ := request["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		msg, _ := item.(map[string]any)
		out = append(out, msg)
	}
	return out
}

func (f *fakeOpenAI) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// waitNewRun waits for a run in threadID other than previousRunID to finish
// and returns it.
func waitNewRun(t *testing.T, c *Container, threadID, previousRunID string) *core.RunRecord {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		run, err := c.store.LoadLatestRunForSession(context.Background(), threadID)
		if err != nil {
			t.Fatalf("latest run: %v", err)
		}
		if run != nil && run.RunID != previousRunID && (run.Status == core.RunStatusSucceeded || run.Status == core.RunStatusFailed) {
			if run.Status == core.RunStatusFailed {
				events, _ := c.store.LoadEvents(context.Background(), run.RunID)
				t.Fatalf("wake run failed: %s", eventKinds(events))
			}
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("no new finished run in thread %s (latest %+v)", threadID, run)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCommitmentWakesAfterRestartAndNotifiesOwner(t *testing.T) {
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	harness, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_w", "schedule_wake", map[string]string{"in": "72h", "task": "提醒 owner 看 X"}),
		textReply("好的"),
		toolCallChunk("call_n", "notify_owner", map[string]string{"title": "提醒", "body": "该看 X 了"}),
		toolCallChunk("call_s", "settle", map[string]any{"id": 1, "action": "done"}),
		textReply("已提醒"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra)
	ctx := context.Background()

	first := harness.open(t, cfg)
	registerPushToken(t, first, "device-token-1")
	thread, err := first.threads.CreateThread(ctx, "x")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := first.runs.CreateRun(ctx, thread.ID, "", "三天后提醒我看 X")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	waitRunStatus(t, first, run.ID, "completed")
	commitments, err := first.store.ListMemoryItems(ctx, []core.MemoryStatus{core.MemoryActive})
	if err != nil {
		t.Fatalf("list memory: %v", err)
	}
	if len(commitments) != 1 || commitments[0].Kind != core.MemoryCommitment || !commitments[0].WakeAt.Equal(start.Add(72*time.Hour)) {
		t.Fatalf("commitments = %+v", commitments)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first container: %v", err)
	}

	second := harness.open(t, cfg)
	defer func() { _ = second.Close() }()
	harness.clock.Set(start.Add(72*time.Hour + time.Minute))
	if err := second.WakeScheduler().Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	woken := waitNewRun(t, second, thread.ID, run.ID)

	messages := requestMessages(provider.request(2))
	var sawWakeInput bool
	for _, msg := range messages {
		if msg["role"] == "user" && strings.Contains(fmt.Sprint(msg["content"]), "[commitment #1, made 2026-10-02 10:00] 提醒 owner 看 X") {
			sawWakeInput = true
		}
	}
	if !sawWakeInput {
		t.Fatalf("wake run input missing from the model request: %v", messages)
	}
	last := messages[len(messages)-1]
	present := fmt.Sprint(last["content"])
	if last["role"] != "system" || !strings.Contains(present, "<presence>") ||
		!strings.Contains(present, "Woken by: commitment #1: 提醒 owner 看 X") ||
		!strings.Contains(present, "#1 [due now") {
		t.Fatalf("last message is not the presence with the woken commitment: %v", last)
	}

	history, err := second.threads.ListMessages(ctx, thread.ID, 20)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var roles []string
	for _, msg := range history {
		roles = append(roles, msg.Role)
		if msg.Role == core.MessageRoleWake && msg.RunID != woken.RunID {
			t.Fatalf("wake message bound to %q, want run %s", msg.RunID, woken.RunID)
		}
	}
	if strings.Join(roles, ",") != "user,assistant,wake,assistant" {
		t.Fatalf("thread roles = %v, want the wake input shown as wake", roles)
	}

	sent := harness.fcm.sent()
	if len(sent) != 1 {
		t.Fatalf("fcm messages = %v, want 1", sent)
	}
	data, _ := sent[0]["data"].(map[string]any)
	if sent[0]["token"] != "device-token-1" || data["thread_id"] != thread.ID || data["run_id"] != woken.RunID {
		t.Fatalf("fcm message = %v, want thread %s run %s", sent[0], thread.ID, woken.RunID)
	}
	item, err := second.store.LoadMemoryItem(ctx, 1)
	if err != nil {
		t.Fatalf("load commitment: %v", err)
	}
	if item.Status != core.MemorySettled {
		t.Fatalf("commitment status = %s, want settled", item.Status)
	}

	if err := second.WakeScheduler().Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if wakes, err := second.store.CountWakesSince(ctx, time.Time{}); err != nil || wakes != 1 {
		t.Fatalf("wakes = %d err=%v, want exactly one", wakes, err)
	}
	if got := provider.requestCount(); got != 5 {
		t.Fatalf("model requests = %d, want 5 (no second wake run)", got)
	}
}

func TestNotificationInQuietHoursWaitsForTheirEnd(t *testing.T) {
	start := time.Date(2026, 10, 5, 23, 30, 0, 0, time.UTC)
	harness, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_n", "notify_owner", map[string]string{"title": "提醒", "body": "明早看 X"}),
		textReply("明早提醒你"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra)
	ctx := context.Background()
	c := harness.open(t, cfg)
	defer func() { _ = c.Close() }()
	registerPushToken(t, c, "device-token-1")
	thread, err := c.threads.CreateThread(ctx, "quiet")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := c.runs.CreateRun(ctx, thread.ID, "", "明早提醒我看 X")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	waitRunStatus(t, c, run.ID, "completed")
	if result, _ := toolMessageFor(provider.request(1), "call_n"); !strings.Contains(result, "queued") {
		t.Fatalf("notify_owner result = %q, want queued", result)
	}
	if sent := harness.fcm.sent(); len(sent) != 0 {
		t.Fatalf("sent during quiet hours: %v", sent)
	}

	harness.clock.Set(time.Date(2026, 10, 6, 8, 1, 0, 0, time.UTC))
	if err := c.WakeScheduler().Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	sent := harness.fcm.sent()
	if len(sent) != 1 {
		t.Fatalf("fcm messages after quiet hours = %v, want 1", sent)
	}
	if data, _ := sent[0]["data"].(map[string]any); data["thread_id"] != thread.ID {
		t.Fatalf("fcm data = %v, want thread %s", data, thread.ID)
	}
}

func TestCommitmentOfDeletedThreadWakesInReminders(t *testing.T) {
	start := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	provider := &fakeOpenAI{replies: []string{textReply("收到")}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "briefing:\n  at: \"\"\n")
	clock := &testClock{now: start}
	ctx := context.Background()
	c, err := buildContainer(ctx, cfg, buildOptions{clock: clock.Now})
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer func() { _ = c.Close() }()
	thread, err := c.threads.CreateThread(ctx, "gone")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := c.store.AddMemoryItem(ctx, core.MemoryItem{
		Kind: core.MemoryCommitment, Status: core.MemoryActive, Content: "看 X",
		SessionID: thread.ID, WakeAt: start.Add(-time.Minute), CreatedAt: start.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("add commitment: %v", err)
	}
	if err := c.threads.DeleteThread(ctx, thread.ID); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	if err := c.WakeScheduler().Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	sessions, err := c.store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != remindersThreadTitle {
		t.Fatalf("sessions = %+v, want only the reminders thread", sessions)
	}
	woken := waitNewRun(t, c, sessions[0].SessionID, "")
	if !strings.Contains(woken.Input, "看 X") {
		t.Fatalf("reminder run input = %q", woken.Input)
	}
}
