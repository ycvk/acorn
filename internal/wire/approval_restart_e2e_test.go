package wire

import (
	"context"
	"encoding/json"
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

// fakeOpenAI replays scripted chat-completion streams in request order and
// records every request body.
type fakeOpenAI struct {
	mu       sync.Mutex
	replies  []string
	requests []map[string]any
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
	f.mu.Unlock()
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
	searchRunsCallStream = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"fake","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_runs","arguments":"{\"query\":\"probe\"}"}}]},"finish_reason":null}]}`
	toolCallsFinish      = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
	doneContent          = `{"id":"c2","object":"chat.completion.chunk","created":2,"model":"fake","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":null}]}`
	stopFinish           = `{"id":"c2","object":"chat.completion.chunk","created":2,"model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
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
    - search_runs
tools:
  workspace:
    root_dir: %s
  run_command:
    disabled: true
`, filepath.Join(dir, "state"), providerURL, dir)
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
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
		{decision: "decline", wantToolText: "The owner declined this search_runs call"},
	} {
		t.Run(tc.decision, func(t *testing.T) {
			provider := &fakeOpenAI{replies: []string{
				sseChunks(searchRunsCallStream, toolCallsFinish),
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
			if _, err := second.PendingAction().Decide(ctx, actionID, api.PendingActionDecisionInput{
				Decision:         tc.decision,
				SelectedOptionID: tc.decision,
			}); err != nil {
				t.Fatalf("decide: %v", err)
			}
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
