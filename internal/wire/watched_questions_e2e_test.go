package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/settings"
)

// The owner asks whether something is out yet. The agent answers, watches the
// question on its own and says so. At the idle thought time one check run in
// a conversation of its own looks it up and records a new answer, leaving the
// owner's conversation alone; the agent then wakes where the owner asked,
// writes the answer there and pushes once.
func TestAWatchedQuestionIsCheckedAndAnsweredWhereTheOwnerAsked(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) // 08:00 in Shanghai
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra, ownerSettings{settings.KeyTimezone: `"Asia/Shanghai"`, settings.KeyBriefingAt: `""`, settings.KeyNightAt: `""`, settings.KeyWanderAt: `["15:00"]`})
	c := h.open(t, cfg)
	defer c.Close()
	registerPushInstallation(t, c, "fid_phone")
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "Qwen")
	if err != nil {
		t.Fatal(err)
	}

	provider.add(
		toolCallChunk("watch", "concern", map[string]any{"action": "create", "title": "Qwen 4 发布了吗", "question": true, "reason": "截至 10 月 12 日还没有发布", "source_id": "__input_source__", "review_at": "2026-10-12T06:00:00Z"}),
		textReply("还没有。这件事我会留意，有变化告诉你。"),
	)
	if _, err := startOwnerRun(ctx, c, thread.ID, "Qwen 4 还没出吗？"); err != nil {
		t.Fatal(err)
	}
	asked := waitNewRun(t, c, thread.ID, "")
	watched := func() core.MemoryConcern {
		t.Helper()
		concerns, err := c.store.ListConcerns(ctx, false)
		if err != nil || len(concerns) != 1 {
			t.Fatalf("concerns = %+v, %v", concerns, err)
		}
		return concerns[0]
	}
	question := watched()
	if !question.Question || question.State != "active" || question.ThreadID != thread.ID || question.Title != "Qwen 4 发布了吗" {
		t.Fatalf("watched question = %+v", question)
	}
	var now api.NowResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/now", nil), http.StatusOK, &now)
	if len(now.Concerns) != 1 || now.Concerns[0].Title != "Qwen 4 发布了吗" {
		t.Fatalf("now page concerns = %+v", now.Concerns)
	}

	// 15:00: the check runs in its own conversation and finds a new answer.
	before := provider.requestCount()
	provider.add(
		toolCallChunk("look", "news_search", map[string]any{"query": "Qwen 4"}),
		toolCallChunk("found", "concern", map[string]any{"action": "resolved", "id": question.ID, "revision": question.Revision, "reason": "今天发布了，见 https://example.com/qwen4", "source_id": "__source:news_search__"}),
		textReply("Qwen 4 发布了吗：有新答案。"),
	)
	h.clock.Set(time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	checks, err := c.store.LatestRoutineThread(ctx, "questions")
	if err != nil || checks == "" || checks == thread.ID {
		t.Fatalf("checks conversation = %q, %v", checks, err)
	}
	waitNewRun(t, c, checks, "")
	check := requestText(provider.request(before))
	if !strings.Contains(check, "[questions 2026-10-12 15:00] Check the questions you are watching for the owner") || !strings.Contains(check, "- concern "+question.ID+" rev 1, asked 2026-10-12: Qwen 4 发布了吗\n  Answer so far: 截至 10 月 12 日还没有发布") {
		t.Fatalf("check input:\n%s", check)
	}
	if strings.Contains(check, "这件事我会留意") {
		t.Fatalf("the check read the owner's conversation:\n%s", check)
	}
	if question = watched(); question.State != "resolved" || question.Revision != 2 {
		t.Fatalf("question after the check = %+v", question)
	}
	if latest, err := c.store.LoadLatestRunForSession(ctx, thread.ID); err != nil || latest.RunID != asked.RunID {
		t.Fatalf("the check ran in the owner's conversation: %+v, %v", latest, err)
	}
	commitments, err := c.store.ListCommitments(ctx, true)
	if err != nil || len(commitments) != 1 || commitments[0].SessionID != thread.ID || commitments[0].Content != "Qwen 4 发布了吗" {
		t.Fatalf("commitments = %+v, %v", commitments, err)
	}
	if sent := h.fcm.sent(); len(sent) != 0 {
		t.Fatalf("the check pushed: %v", sent)
	}

	// The next tick wakes the agent where the owner asked.
	before = provider.requestCount()
	provider.add(
		toolCallChunk("tell", "notify_owner", map[string]string{"title": "Qwen 4 发布了吗", "body": "今天发布了"}),
		toolCallChunk("done", "settle", map[string]any{"id": commitments[0].ID, "occurrence_id": 1, "action": "done", "source_id": "__notify_source__"}),
		textReply("Qwen 4 今天发布了：https://example.com/qwen4"),
	)
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, asked.RunID)
	wake := requestText(provider.request(before))
	for _, want := range []string{"A question the owner asked in this conversation, which you were watching for them, has a new answer.", "Found by your check on 2026-10-12: 今天发布了，见 https://example.com/qwen4", "Qwen 4 还没出吗？"} {
		if !strings.Contains(wake, want) {
			t.Fatalf("the wake lacks %q:\n%s", want, wake)
		}
	}
	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	lines := messageLines(listed)
	if len(lines) < 3 || lines[0] != "user:Qwen 4 还没出吗？" || lines[1] != "assistant:还没有。这件事我会留意，有变化告诉你。" || lines[len(lines)-1] != "assistant:Qwen 4 今天发布了：https://example.com/qwen4" {
		t.Fatalf("messages = %v", lines)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.fcm.sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if sent := h.fcm.sent(); len(sent) != 1 {
		t.Fatalf("pushes = %v", sent)
	}
	if open, err := c.store.ListCommitments(ctx, true); err != nil || len(open) != 0 {
		t.Fatalf("commitments after the answer = %+v, %v", open, err)
	}
	if left, err := c.store.ListConcerns(ctx, true); err != nil || len(left) != 0 {
		t.Fatalf("open concerns after the answer = %+v, %v", left, err)
	}
}
