package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

// TestOwnerMessagesReachTheRunningRun has the owner write while the agent
// works. The message waits, the run takes it before its next model call, and
// the model sees it after the tool result. A message sent while the agent
// writes its last reply waits for that run to end; the run that then starts
// on it is named by the first run's terminal event and sees the reply before
// the message.
func TestOwnerMessagesReachTheRunningRun(t *testing.T) {
	working, replying := make(chan struct{}), make(chan struct{})
	var workOnce, replyOnce sync.Once
	releaseWork := func() { workOnce.Do(func() { close(working) }) }
	releaseReply := func() { replyOnce.Do(func() { close(replying) }) }
	provider := &fakeOpenAI{
		replies: []string{
			toolCallChunk("look", "recall", map[string]string{"query": "上海天气"}),
			textReply("上海今天晴，18 到 24 度。"),
			textReply("明天多云，16 到 21 度。"),
		},
		gates: map[int]chan struct{}{0: working, 1: replying},
	}
	server := httptest.NewServer(provider)
	defer server.Close()
	defer releaseReply()
	defer releaseWork()
	cfg := writeTestConfig(t, server.URL, "")
	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "天气")
	if err != nil {
		t.Fatal(err)
	}
	runs := "/v1/threads/" + thread.ID + "/runs"

	var started api.CreateRunResponse
	client.postJSON(t, runs, map[string]any{"input": "查一下上海天气"}, http.StatusCreated, &started)
	waitModelRequests(t, provider, 1)

	var waiting api.CreateRunResponse
	client.postJSON(t, runs, map[string]any{"input": "只要气温就行"}, http.StatusAccepted, &waiting)
	if waiting.Run.ID != started.Run.ID || waiting.Run.Status != "running" || waiting.Queued == nil || waiting.Queued.Text != "只要气温就行" {
		t.Fatalf("waiting message = %+v, want it queued for run %s", waiting, started.Run.ID)
	}
	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if got := messageLines(listed); !slices.Equal(got, []string{"user:查一下上海天气"}) || len(listed.Queued) != 1 || listed.Queued[0].ID != waiting.Queued.ID {
		t.Fatalf("while it waits: messages = %v, queued = %+v", got, listed.Queued)
	}

	releaseWork()
	waitModelRequests(t, provider, 2)
	messages := requestMessages(provider.request(1))
	tool := slices.IndexFunc(messages, func(m map[string]any) bool { return m["role"] == "tool" })
	owner := slices.IndexFunc(messages, func(m map[string]any) bool {
		return m["role"] == "user" && chatContentText(m["content"]) == "只要气温就行"
	})
	if tool < 0 || owner < tool {
		t.Fatalf("the model call after the tool result sends the owner's message at %d, tool result at %d", owner, tool)
	}

	var late api.CreateRunResponse
	client.postJSON(t, runs, map[string]any{"input": "那明天呢"}, http.StatusAccepted, &late)
	if late.Run.ID != started.Run.ID || late.Queued == nil {
		t.Fatalf("late message = %+v", late)
	}
	releaseReply()
	waitRunStatus(t, c, started.Run.ID, "completed")

	var detail api.RunDetailDTO
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/runs/"+started.Run.ID+"/detail", nil), http.StatusOK, &detail)
	var next string
	var taken []string
	for _, event := range detail.Events {
		data, _ := event.Data.(map[string]any)
		switch event.Type {
		case "run.input":
			taken = append(taken, data["queued_id"].(string)+":"+data["text"].(string))
		case "run.completed":
			next, _ = data["next_run_id"].(string)
		}
	}
	if !slices.Equal(taken, []string{waiting.Queued.ID + ":只要气温就行"}) || next == "" {
		t.Fatalf("run events: taken = %v, next run = %q", taken, next)
	}
	waitRunStatus(t, c, next, "completed")
	if got := lastOwnerInput(provider.request(2)); got != "那明天呢" {
		t.Fatalf("the next run's input = %q", got)
	}

	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	want := []string{"user:查一下上海天气", "user:只要气温就行", "assistant:上海今天晴，18 到 24 度。", "user:那明天呢", "assistant:明天多云，16 到 21 度。"}
	if got := messageLines(listed); !slices.Equal(got, want) || len(listed.Queued) != 0 {
		t.Fatalf("messages = %v, queued = %+v", got, listed.Queued)
	}
	if listed.Items[1].RunID != started.Run.ID || listed.Items[3].RunID != next {
		t.Fatalf("message runs = %s, %s; want %s, %s", listed.Items[1].RunID, listed.Items[3].RunID, started.Run.ID, next)
	}
	// What the owner said mid-run is a source of the run that took it.
	sources, err := c.store.RunMemorySources(ctx, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(sources, func(s core.MemorySource) bool { return s.ID == "message:"+listed.Items[1].ID && s.Speaker == "owner" }) {
		t.Fatalf("run sources = %+v", sources)
	}
}

func messageLines(listed api.MessageListResponse) []string {
	lines := make([]string, len(listed.Items))
	for i, item := range listed.Items {
		lines[i] = item.Role + ":" + item.Content.Text
	}
	return lines
}

func waitModelRequests(t *testing.T, provider *fakeOpenAI, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for provider.requestCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("model requests = %d, want %d", provider.requestCount(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
