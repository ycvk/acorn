package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

// routedOpenAI answers each chat completion from what the request carries,
// for runs whose model calls interleave.
type routedOpenAI struct {
	mu       sync.Mutex
	requests []map[string]any
	respond  func(request map[string]any) string
}

func (f *routedOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	f.mu.Unlock()
	reply := f.respond(request)
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, reply)
}

func (f *routedOpenAI) recorded() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

const subAgentPrefix = "You are a sub-agent of Acorn"

func isSubRunRequest(request map[string]any) bool {
	messages := requestMessages(request)
	return len(messages) > 0 && messages[0]["role"] == "system" && strings.HasPrefix(chatContentText(messages[0]["content"]), subAgentPrefix)
}

// userTexts joins the user messages of a request.
func userTexts(request map[string]any) string {
	var out []string
	for _, msg := range requestMessages(request) {
		if msg["role"] == "user" {
			out = append(out, chatContentText(msg["content"]))
		}
	}
	return strings.Join(out, "\n")
}

type modelCall struct {
	name string
	args map[string]any
}

// modelCalls lists the tool calls of the request's assistant messages.
func modelCalls(request map[string]any) []modelCall {
	var calls []modelCall
	for _, msg := range requestMessages(request) {
		raw, _ := msg["tool_calls"].([]any)
		for _, item := range raw {
			function, _ := item.(map[string]any)["function"].(map[string]any)
			var args map[string]any
			_ = json.Unmarshal([]byte(function["arguments"].(string)), &args)
			calls = append(calls, modelCall{name: function["name"].(string), args: args})
		}
	}
	return calls
}

func countCalls(calls []modelCall, name, action string, task float64) int {
	n := 0
	for _, call := range calls {
		if call.name == name && call.args["action"] == action && (task == 0 || call.args["task"] == task) {
			n++
		}
	}
	return n
}

func requestTools(request map[string]any) []string {
	raw, _ := request["tools"].([]any)
	var names []string
	for _, item := range raw {
		function, _ := item.(map[string]any)["function"].(map[string]any)
		names = append(names, function["name"].(string))
	}
	return names
}

// The agent hands one article to a sub-run and the check of its summary to
// another. The sub-runs see only their briefs and start once the agent has
// said it waits; the run waits for them, takes their reports, accepts the
// summary, sends the check back once and accepts the second attempt. Only
// then does it end, with its last reply as the conversation's message.
func TestDelegatedWorkReportsBeforeTheRunEnds(t *testing.T) {
	waiting := make(chan struct{})
	var waitOnce sync.Once
	provider := &routedOpenAI{}
	provider.respond = func(request map[string]any) string {
		users := userTexts(request)
		if isSubRunRequest(request) {
			<-waiting
			if slices.ContainsFunc(requestMessages(request), func(m map[string]any) bool { return m["role"] == "tool" }) {
				return textReply("交回了")
			}
			submission := map[string]any{"summary": "A 的三条要点", "evidence": []string{"读了 https://example.com/a 全文"}}
			switch {
			case strings.Contains(users, "[attempt 1 was sent back]"):
				submission = map[string]any{"summary": "复核通过", "evidence": []string{"逐条对照了原文"}}
			case strings.Contains(users, "Objective: 复核"):
				submission = map[string]any{"summary": "看起来没问题", "limitations": []string{"没打开原文"}}
			}
			return toolCallChunk("submit_1", "submit", submission)
		}
		calls := modelCalls(request)
		switch {
		case countCalls(calls, "delegate", "start", 0) == 0:
			return toolCallChunk("start_a", "delegate", map[string]any{"action": "start", "objective": "读 A: https://example.com/a", "deliverables": []string{"三条要点"}, "acceptance": []string{"每条带原文出处"}})
		case countCalls(calls, "delegate", "start", 0) == 1:
			return toolCallChunk("start_b", "delegate", map[string]any{"action": "start", "objective": "复核 A 的三条要点", "acceptance": []string{"对照原文"}, "block": []string{"files"}})
		case strings.Contains(users, "[sub_run task 2, attempt 1: succeeded]") && countCalls(calls, "delegate", "redo", 2) == 0:
			return toolCallChunk("redo_b", "delegate", map[string]any{"action": "redo", "task": 2, "feedback": "要对照原文"})
		case strings.Contains(users, "[sub_run task 1, attempt 1: succeeded]") && countCalls(calls, "delegate", "accept", 1) == 0:
			return toolCallChunk("accept_a", "delegate", map[string]any{"action": "accept", "task": 1})
		case strings.Contains(users, "[sub_run task 2, attempt 2: succeeded]") && countCalls(calls, "delegate", "accept", 2) == 0:
			return toolCallChunk("accept_b", "delegate", map[string]any{"action": "accept", "task": 2})
		case countCalls(calls, "delegate", "accept", 1) == 1 && countCalls(calls, "delegate", "accept", 2) == 1:
			return textReply("汇总：A 的三条要点，复核通过。")
		default:
			defer waitOnce.Do(func() { close(waiting) })
			return textReply("两个子任务在跑，等它们回来。")
		}
	}
	server := httptest.NewServer(provider)
	defer server.Close()
	defer waitOnce.Do(func() { close(waiting) })
	c, err := NewContainer(context.Background(), writeTestConfig(t, server.URL, ""))
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "读文章")
	if err != nil {
		t.Fatal(err)
	}
	var started api.CreateRunResponse
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "读一下 A 再找人复核"}, http.StatusCreated, &started)
	waitRunStatusWithin(t, c, started.Run.ID, "completed", 30*time.Second)

	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if got := messageLines(listed); !slices.Equal(got, []string{"user:读一下 A 再找人复核", "assistant:汇总：A 的三条要点，复核通过。"}) {
		t.Fatalf("messages = %v", got)
	}
	latest, err := c.store.LoadLatestRunForSession(ctx, thread.ID)
	if err != nil || latest.RunID != started.Run.ID {
		t.Fatalf("latest run of the thread = %+v, %v", latest, err)
	}
	attempts, err := c.store.SubRuns(ctx, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, sub := range attempts {
		got = append(got, fmt.Sprintf("%d/%d %s %s %s delivered=%t", sub.Task, sub.Attempt, sub.Status, sub.Verdict, sub.Feedback, sub.Delivered))
		run, err := c.store.LoadRun(ctx, sub.RunID)
		if err != nil || run.SessionID != thread.ID {
			t.Fatalf("sub-run %s = %+v, %v", sub.RunID, run, err)
		}
	}
	want := []string{
		"1/1 succeeded accepted  delivered=true",
		"2/1 succeeded redo 要对照原文 delivered=true",
		"2/2 succeeded accepted  delivered=true",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sub-runs = %v", got)
	}
	if attempts[2].Submission == nil || attempts[2].Submission.Summary != "复核通过" {
		t.Fatalf("second check submitted %+v", attempts[2].Submission)
	}

	// The reply lists each task's latest attempt; the run's events told the
	// owner how each task went as it went.
	wantTasks := []api.SubRunData{
		{Task: 1, Attempt: 1, State: "accepted", Objective: "读 A: https://example.com/a", Summary: "A 的三条要点"},
		{Task: 2, Attempt: 2, State: "accepted", Objective: "复核 A 的三条要点", Summary: "复核通过"},
	}
	if reply := listed.Items[1]; !slices.Equal(reply.SubRuns, wantTasks) {
		t.Fatalf("the reply's tasks = %+v, want %+v", reply.SubRuns, wantTasks)
	}
	events, err := c.store.LoadEvents(ctx, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	updates := map[int][]string{}
	for _, record := range events {
		if record.Kind != core.EventSubRunUpdated {
			continue
		}
		event, err := api.ProjectRunEvent(record)
		if err != nil {
			t.Fatalf("project %s: %v", record.Kind, err)
		}
		data := event.Data.(api.SubRunData)
		updates[data.Task] = append(updates[data.Task], fmt.Sprintf("%d %s %s", data.Attempt, data.State, data.Summary))
	}
	if got, want := updates[2], []string{"1 running ", "1 reported 看起来没问题", "2 running ", "2 reported 复核通过", "2 accepted 复核通过"}; !slices.Equal(got, want) {
		t.Fatalf("task 2 updates = %q, want %q", got, want)
	}
	if got, want := updates[1], []string{"1 running ", "1 reported A 的三条要点", "1 accepted A 的三条要点"}; !slices.Equal(got, want) {
		t.Fatalf("task 1 updates = %q, want %q", got, want)
	}

	var sawParent, sawReport bool
	for _, request := range provider.recorded() {
		tools := requestTools(request)
		users := userTexts(request)
		if !isSubRunRequest(request) {
			sawParent = true
			sawReport = sawReport || strings.Contains(users, "[sub_run task 1, attempt 1: succeeded] A sub-agent's report, not the owner's words") &&
				strings.Contains(users, "Objective: 读 A: https://example.com/a\nSummary: A 的三条要点")
			if !slices.Contains(tools, "delegate") || slices.Contains(tools, "submit") {
				t.Fatalf("the run's tools = %v", tools)
			}
			continue
		}
		if strings.Contains(users, "读一下 A 再找人复核") || !strings.HasPrefix(users, "[brief]\nObjective: ") {
			t.Fatalf("a sub-run saw more than its brief:\n%s", users)
		}
		if !slices.Contains(tools, "submit") || !slices.Contains(tools, "web_fetch") {
			t.Fatalf("sub-run tools = %v", tools)
		}
		for _, name := range []string{"delegate", "notify_owner", "keep", "recall", "schedule_wake", "goal", "knowledge_write"} {
			if slices.Contains(tools, name) {
				t.Fatalf("a sub-run got %s: %v", name, tools)
			}
		}
		if strings.Contains(users, "Objective: 复核") && !strings.Contains(users, "Tools you may not call: files") {
			t.Fatalf("the check's brief lacks its block:\n%s", users)
		}
		if strings.Contains(users, "[attempt 1 was sent back]") && (!strings.Contains(users, "Summary: 看起来没问题") || !strings.Contains(users, "Feedback: 要对照原文")) {
			t.Fatalf("the redo lacks the first attempt or the feedback:\n%s", users)
		}
	}
	if !sawParent || !sawReport {
		t.Fatalf("the run saw no report (parent requests: %t)", sawParent)
	}
}

// Stopping a run that waits for its sub-runs stops them too.
func TestStoppingTheRunStopsItsSubRuns(t *testing.T) {
	hold := make(chan struct{})
	var holdOnce sync.Once
	asked := make(chan struct{}, 1)
	provider := &routedOpenAI{}
	provider.respond = func(request map[string]any) string {
		if isSubRunRequest(request) {
			asked <- struct{}{}
			<-hold
			return textReply("晚了")
		}
		if countCalls(modelCalls(request), "delegate", "start", 0) == 0 {
			return toolCallChunk("start_a", "delegate", map[string]any{"action": "start", "objective": "读很长的 A"})
		}
		return textReply("等它回来。")
	}
	server := httptest.NewServer(provider)
	defer server.Close()
	defer holdOnce.Do(func() { close(hold) })
	c, err := NewContainer(context.Background(), writeTestConfig(t, server.URL, ""))
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "读文章")
	if err != nil {
		t.Fatal(err)
	}
	var started api.CreateRunResponse
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "读 A"}, http.StatusCreated, &started)
	select {
	case <-asked:
	case <-time.After(15 * time.Second):
		t.Fatal("the sub-run never called the model")
	}
	waitFor(t, "the run waits for its sub-run", func() bool {
		events, err := c.store.LoadEvents(ctx, started.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(events, func(e core.EventRecord) bool {
			message, _ := e.Payload.(map[string]any)["message"].(map[string]any)
			return e.Kind == "runtime.message" && message["content"] == "等它回来。"
		})
	})
	if err := c.runs.InterruptRun(ctx, started.Run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatusWithin(t, c, started.Run.ID, "failed", 15*time.Second)
	attempts, err := c.store.SubRuns(ctx, started.Run.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("sub-runs = %+v, %v", attempts, err)
	}
	waitFor(t, "the sub-run stops", func() bool {
		run, err := c.store.LoadRun(ctx, attempts[0].RunID)
		if err != nil {
			t.Fatal(err)
		}
		return run.Status == core.RunStatusFailed && strings.Contains(run.Error, "the run that delegated it ended")
	})
}

func waitRunStatusWithin(t *testing.T, c *Container, runID, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		run, err := c.runs.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status == want {
			return
		}
		if (run.Status == "failed" && want != "failed") || time.Now().After(deadline) {
			events, _ := c.store.LoadEvents(context.Background(), runID)
			record, _ := c.store.LoadRun(context.Background(), runID)
			t.Fatalf("run %s status = %s (%+v), want %s; events: %s", runID, run.Status, record, want, eventKinds(events))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A sub-run goes on while the run that delegated it waits for the owner's
// approval; its report reaches the run once the owner decides.
func TestSubRunsReportAfterTheRunsApproval(t *testing.T) {
	// The sub-run starts once the run asked for the call that waits, so its
	// report cannot reach the run before the approval.
	gated := make(chan struct{})
	var gateOnce sync.Once
	provider := &routedOpenAI{}
	provider.respond = func(request map[string]any) string {
		if isSubRunRequest(request) {
			<-gated
			if slices.ContainsFunc(requestMessages(request), func(m map[string]any) bool { return m["role"] == "tool" }) {
				return textReply("交回了")
			}
			return toolCallChunk("submit_1", "submit", map[string]any{"summary": "A 的三条要点"})
		}
		calls, users := modelCalls(request), userTexts(request)
		switch {
		case countCalls(calls, "delegate", "start", 0) == 0:
			return toolCallChunk("start_a", "delegate", map[string]any{"action": "start", "objective": "读 A"})
		case !slices.ContainsFunc(calls, func(call modelCall) bool { return call.name == "recall" }):
			defer gateOnce.Do(func() { close(gated) })
			return toolCallChunk("recall_1", "recall", map[string]string{"query": "上次读的 A"})
		case strings.Contains(users, "[sub_run task 1, attempt 1: succeeded]") && countCalls(calls, "delegate", "accept", 1) == 0:
			return toolCallChunk("accept_a", "delegate", map[string]any{"action": "accept", "task": 1})
		case countCalls(calls, "delegate", "accept", 1) == 1:
			return textReply("A 读完了。")
		default:
			return textReply("等它回来。")
		}
	}
	server := httptest.NewServer(provider)
	defer server.Close()
	defer gateOnce.Do(func() { close(gated) })
	c, err := NewContainer(context.Background(), writeApprovalTestConfig(t, server.URL))
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "读文章")
	if err != nil {
		t.Fatal(err)
	}
	var started api.CreateRunResponse
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "读 A，顺便查查上次读到哪"}, http.StatusCreated, &started)
	waitRunStatus(t, c, started.Run.ID, "interrupted")
	waitFor(t, "the sub-run reports while the run waits for approval", func() bool {
		attempts, err := c.store.SubRuns(ctx, started.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(attempts) == 1 && attempts[0].Status == core.RunStatusSucceeded && !attempts[0].Delivered
	})
	actions, err := c.pendingAction.List(ctx, 10)
	if err != nil || len(actions) != 1 || actions[0].RunID != started.Run.ID {
		t.Fatalf("pending actions = %+v, %v", actions, err)
	}
	decideOverHTTP(t, c, actions[0].ActionID, "accept")
	waitRunStatus(t, c, started.Run.ID, "completed")
	attempts, err := c.store.SubRuns(ctx, started.Run.ID)
	if err != nil || len(attempts) != 1 || attempts[0].Verdict != core.SubRunAccepted || !attempts[0].Delivered {
		t.Fatalf("sub-runs = %+v, %v", attempts, err)
	}
	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if got := messageLines(listed); !slices.Equal(got, []string{"user:读 A，顺便查查上次读到哪", "assistant:A 读完了。"}) {
		t.Fatalf("messages = %v", got)
	}
}
