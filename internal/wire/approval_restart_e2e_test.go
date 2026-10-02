package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

// fakeOpenAI replays scripted chat-completion streams in request order and
// records every request body.
type fakeOpenAI struct {
	mu       sync.Mutex
	replies  []string
	requests []map[string]any
	// gates holds a reply until the test closes the channel for that request.
	gates map[int]chan struct{}
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, request)
	index := len(f.requests) - 1
	gate := f.gates[index]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if index >= len(f.replies) {
		http.Error(w, fmt.Sprintf("no scripted reply for request %d", index), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, f.replies[index])
}

func (f *fakeOpenAI) request(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.requests) {
		return nil
	}
	return f.requests[i]
}

func sseChunks(chunks ...string) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString("data: ")
		b.WriteString(chunk)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

const (
	recallCallStream = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"fake","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"recall","arguments":"{\"query\":\"probe\"}"}}]},"finish_reason":null}]}`
	toolCallsFinish  = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
	doneContent      = `{"id":"c2","object":"chat.completion.chunk","created":2,"model":"fake","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":null}]}`
	stopFinish       = `{"id":"c2","object":"chat.completion.chunk","created":2,"model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
)

func writeApprovalTestConfig(t *testing.T, providerURL string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "acorn.yaml")
	yaml := fmt.Sprintf(`runtime:
  storage_dir: %s
providers:
  - name: primary
    model: fake
    base_url: %s/v1
    api_key: test
    timeout_seconds: 10
    max_completion_tokens: 512
    enabled: true
approval:
  require:
    - recall
tools:
  workspace:
    root_dir: %s
`, filepath.Join(dir, "state"), providerURL, dir)
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := os.MkdirAll(cfg.Runtime.StorageDir, 0o700); err != nil {
		t.Fatalf("create storage dir: %v", err)
	}
	if err := os.WriteFile(cfg.PersonaPath(), []byte(presence.DefaultPersona), 0o600); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	return cfg
}

// decideOverHTTP decides a pending action through the /v1 handler the way the
// mobile app does, so the action ID has to survive the :decide route.
func decideOverHTTP(t *testing.T, c *Container, actionID, decision string) {
	t.Helper()
	ctx := context.Background()
	handler, err := api.NewHandler(api.Dependencies{
		Threads:       c.Threads(),
		Runs:          c.Runs(),
		Events:        c.Events(),
		PendingAction: c.PendingAction(),
		Memory:        c.Memory(),
		Skills:        c.Skills(),
		Capabilities:  c.Capabilities(),
		DeviceAuth:    c.DeviceAuth(),
		Inbox:         c.Inbox(),
		Config:        c.Config(),
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("api handler: %v", err)
	}
	code, err := c.DeviceAuth().CreatePairingCode(ctx, time.Minute)
	if err != nil {
		t.Fatalf("pairing code: %v", err)
	}
	paired, err := c.DeviceAuth().PairDevice(ctx, api.PairDeviceInput{PairingCode: code.Code, DeviceName: "test", Platform: "android"})
	if err != nil {
		t.Fatalf("pair device: %v", err)
	}
	body := fmt.Sprintf(`{"decision":%q,"selected_option_id":%q}`, decision, decision)
	req := httptest.NewRequest(http.MethodPost, "/v1/pending-actions/"+url.PathEscape(actionID)+":decide", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+paired.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide %s over HTTP: status %d body %s", actionID, rec.Code, rec.Body.String())
	}
}

func waitRunStatus(t *testing.T, c *Container, runID, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		run, err := c.Runs().GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status == want {
			return
		}
		if run.Status == "failed" || time.Now().After(deadline) {
			events, _ := c.store.LoadEvents(context.Background(), runID)
			t.Fatalf("run %s status = %s, want %s; events: %s", runID, run.Status, want, eventKinds(events))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func eventKinds(events []core.EventRecord) string {
	parts := make([]string, 0, len(events))
	for _, e := range events {
		parts = append(parts, e.Kind)
		if e.Kind == "run.failed" || e.Kind == "tool.call.succeeded" {
			parts = append(parts, fmt.Sprintf("%v", e.Payload))
		}
	}
	return strings.Join(parts, ", ")
}

// startApprovalRun drives a run to the tool_approval interrupt with the first
// container, closes it, and returns the run id and pending action id.
func startApprovalRun(t *testing.T, cfg *config.Config) (runID, actionID string) {
	t.Helper()
	ctx := context.Background()
	first, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("first container: %v", err)
	}
	thread, err := first.Threads().CreateThread(ctx, "approval")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := first.Runs().CreateRun(ctx, thread.ID, "", "find probe")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	waitRunStatus(t, first, run.ID, "interrupted")
	actions, err := first.PendingAction().List(ctx, 10)
	if err != nil {
		t.Fatalf("list pending actions: %v", err)
	}
	if len(actions) != 1 || actions[0].Kind != "tool_approval" || actions[0].RunID != run.ID {
		t.Fatalf("pending actions = %+v", actions)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first container: %v", err)
	}
	return run.ID, actions[0].ActionID
}

func toolMessageFor(request map[string]any, callID string) (string, bool) {
	messages, _ := request["messages"].([]any)
	for _, raw := range messages {
		msg, _ := raw.(map[string]any)
		if msg["role"] == "tool" && msg["tool_call_id"] == callID {
			content, _ := msg["content"].(string)
			return content, true
		}
	}
	return "", false
}

func TestApprovalSurvivesRestart(t *testing.T) {
	for _, tc := range []struct {
		decision     string
		wantToolText string
		wantExecuted bool
	}{
		{decision: "accept", wantExecuted: true},
		{decision: "decline", wantToolText: "The owner declined this recall call"},
	} {
		t.Run(tc.decision, func(t *testing.T) {
			provider := &fakeOpenAI{replies: []string{
				sseChunks(recallCallStream, toolCallsFinish),
				sseChunks(doneContent, stopFinish),
			}}
			server := httptest.NewServer(provider)
			defer server.Close()
			cfg := writeApprovalTestConfig(t, server.URL)
			runID, actionID := startApprovalRun(t, cfg)

			ctx := context.Background()
			second, err := NewContainer(ctx, cfg)
			if err != nil {
				t.Fatalf("second container: %v", err)
			}
			defer func() { _ = second.Close() }()
			decideOverHTTP(t, second, actionID, tc.decision)
			waitRunStatus(t, second, runID, "completed")

			resumed := provider.request(1)
			if resumed == nil {
				t.Fatal("the resumed run never called the model")
			}
			content, ok := toolMessageFor(resumed, "call_1")
			if !ok {
				t.Fatalf("resumed request has no tool message for call_1: %v", resumed["messages"])
			}
			if tc.wantToolText != "" && !strings.Contains(content, tc.wantToolText) {
				t.Fatalf("tool message = %q, want %q", content, tc.wantToolText)
			}
			if tc.wantExecuted && strings.Contains(content, "declined") {
				t.Fatalf("accepted call was not executed: %q", content)
			}

			record, err := second.store.LoadRun(ctx, runID)
			if err != nil {
				t.Fatalf("load run: %v", err)
			}
			if record.Output != "done" {
				t.Fatalf("run output = %q, want done", record.Output)
			}
			if _, ok, err := second.store.LoadCheckpoint(ctx, runID); err != nil || ok {
				t.Fatalf("checkpoint after completion: present=%v err=%v", ok, err)
			}
		})
	}
}

// TestDecidedRunResumesFromSweepAndShowsRunning covers a decision that was
// saved but whose resume never started (the process exited right after the
// decision): the startup sweep resumes it, and while it runs the run is
// visible as running.
func TestDecidedRunResumesFromSweepAndShowsRunning(t *testing.T) {
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	provider := &fakeOpenAI{
		replies: []string{
			sseChunks(recallCallStream, toolCallsFinish),
			sseChunks(doneContent, stopFinish),
		},
		gates: map[int]chan struct{}{1: gate},
	}
	server := httptest.NewServer(provider)
	defer server.Close()
	// Release the held reply before the server closes, also when an
	// assertion below fails, so Close does not wait on the handler forever.
	defer release()
	cfg := writeApprovalTestConfig(t, server.URL)
	runID, actionID := startApprovalRun(t, cfg)

	ctx := context.Background()
	second, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("second container: %v", err)
	}
	defer func() { _ = second.Close() }()
	if _, err := second.store.DecidePendingAction(ctx, actionID, core.PendingActionStatusApproved, `{"action":"accept"}`); err != nil {
		t.Fatalf("decide without resume: %v", err)
	}
	waitRunStatus(t, second, runID, "interrupted")

	if err := second.ResumeReadyRuns(ctx); err != nil {
		t.Fatalf("resume ready runs: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for provider.request(1) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the swept run never called the model")
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := second.Runs().GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("status during resume = %s, want running", run.Status)
	}
	inbox, err := second.Inbox().Load(ctx)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(inbox.ActiveRuns) != 1 || inbox.ActiveRuns[0].RunID != runID {
		t.Fatalf("inbox active runs = %+v, want the resumed run", inbox.ActiveRuns)
	}
	release()
	waitRunStatus(t, second, runID, "completed")
}

// TestBrokenAssistantStreamFailsRunThroughNormalPath covers a provider stream
// that breaks mid-response: the run must end failed with one run.failed event
// and leave no checkpoint behind.
func TestBrokenAssistantStreamFailsRunThroughNormalPath(t *testing.T) {
	partial := `{"id":"c3","object":"chat.completion.chunk","created":3,"model":"fake","choices":[{"index":0,"delta":{"role":"assistant","content":"half an ans"},"finish_reason":null}]}`
	provider := &fakeOpenAI{replies: []string{"data: " + partial + "\n\ndata: {not json\n\n"}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeApprovalTestConfig(t, server.URL)
	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer func() { _ = c.Close() }()
	thread, err := c.Threads().CreateThread(ctx, "broken stream")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := c.Runs().CreateRun(ctx, thread.ID, "", "say something")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		current, err := c.Runs().GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if current.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run status = %s, want failed", current.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	events, err := c.store.LoadEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	failed := 0
	for _, e := range events {
		if e.Kind == "run.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("run.failed events = %d, want 1; events: %s", failed, eventKinds(events))
	}
	if _, ok, err := c.store.LoadCheckpoint(ctx, run.ID); err != nil || ok {
		t.Fatalf("checkpoint after failure: present=%v err=%v", ok, err)
	}
	record, err := c.store.LoadRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if record.Output != "half an ans" {
		t.Fatalf("run output = %q, want the streamed partial output kept by the normal failure path", record.Output)
	}
}
