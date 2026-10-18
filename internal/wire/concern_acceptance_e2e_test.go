package wire

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// The owner hands over two matters in two conversations and two idle-thought
// slots advance them in turn, each in the conversation it came from; a
// deleted conversation takes its concern with it.
func TestWanderAdvancesEachConcernInTheConversationItCameFrom(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) // 08:00 in Shanghai
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+"owner:\n  timezone: Asia/Shanghai\nbriefing:\n  at: \"\"\nthinking:\n  night_at: \"\"\n  wander_at: [\"10:00\", \"15:00\"]\n")
	c := h.open(t, cfg)
	defer c.Close()
	client := newPairedClient(t, c)

	threads, latest := map[string]string{}, map[string]string{}
	for _, matter := range []struct{ key, title string }{{"a", "跟进 A 项目的发布"}, {"b", "跟进 B 项目的发布"}} {
		thread, err := c.threads.CreateThread(ctx, matter.key)
		if err != nil {
			t.Fatal(err)
		}
		provider.add(
			toolCallChunk("create_"+matter.key, "concern", map[string]string{"action": "create", "title": matter.title, "reason": "owner 交代持续跟进", "source_id": "__input_source__"}),
			textReply("好的，我会持续跟进。"),
		)
		if _, err := c.runs.CreateRun(ctx, thread.ID, "", "帮我"+matter.title, nil); err != nil {
			t.Fatal(err)
		}
		threads[matter.key], latest[matter.key] = thread.ID, waitNewRun(t, c, thread.ID, "").RunID
		h.clock.Set(h.clock.Now().Add(time.Minute))
	}
	concern := func(key string) core.MemoryConcern {
		t.Helper()
		concerns, err := c.store.ListConcerns(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range concerns {
			if item.ThreadID == threads[key] {
				return item
			}
		}
		t.Fatalf("no concern in conversation %s: %+v", key, concerns)
		return core.MemoryConcern{}
	}
	wander := func(key string, at time.Time, args map[string]any, reply string) map[string]any {
		t.Helper()
		before := provider.requestCount()
		provider.add(toolCallChunk("advance_"+key, "concern", args), textReply(reply))
		h.clock.Set(at)
		if err := c.wake.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		latest[key] = waitNewRun(t, c, threads[key], latest[key]).RunID
		return provider.request(before)
	}
	advance := func(item core.MemoryConcern, reason string) map[string]any {
		return map[string]any{"action": "active", "id": item.ID, "revision": item.Revision, "title": item.Title, "reason": reason, "source_id": "__input_source__"}
	}

	a := concern("a")
	request := wander("a", time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC), advance(a, "读完 v1.4 的发布说明，下一步看 v1.5"), "A 第一次推进：读完了 v1.4 的发布说明。")
	if text := requestText(request); !strings.Contains(text, "[wander 2026-10-12 10:00] Advance concern "+a.ID+" rev 1") {
		t.Fatalf("first slot did not advance A in its conversation:\n%s", text)
	}
	b := concern("b")
	request = wander("b", time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC), advance(b, "B 还没有新版本"), "B 第一次推进：还没有新版本。")
	if text := requestText(request); !strings.Contains(text, "[wander 2026-10-12 15:00] Advance concern "+b.ID+" rev 1") || strings.Contains(text, "A 第一次推进") {
		t.Fatalf("second slot did not advance B in its own conversation:\n%s", text)
	}

	a = concern("a")
	next := advance(a, "v1.5 的不兼容变更已列出")
	next["review_at"] = "2026-10-20T10:00:00+08:00"
	request = wander("a", time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC), next, "A 第二次推进：列出了 v1.5 的不兼容变更。")
	text := requestText(request)
	if !strings.Contains(text, "[wander 2026-10-13 10:00] Advance concern "+a.ID+" rev 2 [active]: 跟进 A 项目的发布\nLatest reason: 读完 v1.4 的发布说明") || !strings.Contains(text, "A 第一次推进：读完了 v1.4 的发布说明。") {
		t.Fatalf("the second advance of A did not continue from the first:\n%s", text)
	}
	if strings.Contains(text, "B 第一次推进") {
		t.Fatalf("A's conversation carries B's progress:\n%s", text)
	}

	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/threads/"+threads["b"], nil), http.StatusNoContent, nil)
	now := loadNow(t, client)
	if len(now.Concerns) != 1 || now.Concerns[0].ID != a.ID {
		t.Fatalf("now page concerns after deleting B's conversation = %+v", now.Concerns)
	}
	calls := provider.requestCount()
	h.clock.Set(time.Date(2026, 10, 13, 7, 0, 0, 0, time.UTC))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.requestCount() != calls {
		t.Fatal("a slot advanced something after B's conversation was deleted while A waits for review")
	}
	if concerns, err := c.store.ListConcerns(ctx, false); err != nil || len(concerns) != 1 || concerns[0].ID != a.ID {
		t.Fatalf("concerns after deleting B's conversation = %+v, %v", concerns, err)
	}
}

// The owner asks for a document. Idle-thought slots work on it in the
// workstation's directory for the concern, revising the concern each time; a
// command that needs approval waits for the owner and continues once
// approved. The document is sent in the owner's conversation with one push,
// the concern waits, the agent cannot resolve it on its own, and the owner's
// confirmation resolves it.
func TestWanderDeliversAnOutcomeThatOnlyTheOwnerResolves(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken, Supervisor: workstation.NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) // 08:00 in Shanghai
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+fmt.Sprintf("workstation:\n  url: %s\n  token: %s\napproval:\n  require:\n    - shell\nowner:\n  timezone: Asia/Shanghai\nbriefing:\n  at: \"\"\nthinking:\n  night_at: \"\"\n  wander_at: [\"10:00\", \"15:00\"]\n", wsServer.URL, fileTestToken))
	c := h.open(t, cfg)
	defer c.Close()
	client := newPairedClient(t, c)
	registerPushToken(t, c, "tok_phone")

	thread, err := c.threads.CreateThread(ctx, "X 的版本变化")
	if err != nil {
		t.Fatal(err)
	}
	const outcome = "一份 Markdown，列出 v1.4 到 v1.5 的不兼容变更和升级步骤"
	provider.add(
		toolCallChunk("create", "concern", map[string]string{"action": "create", "title": "整理 X 近几个版本的变化", "outcome": outcome, "reason": "owner 交代整理成文档", "source_id": "__input_source__"}),
		textReply("好的，我会整理好发给你。"),
	)
	if _, err := c.runs.CreateRun(ctx, thread.ID, "", "把 X 近几个版本的变化整理成一份 Markdown", nil); err != nil {
		t.Fatal(err)
	}
	latest := waitNewRun(t, c, thread.ID, "").RunID
	concern := func() core.MemoryConcern {
		t.Helper()
		concerns, err := c.store.ListConcerns(ctx, false)
		if err != nil || len(concerns) != 1 || concerns[0].ThreadID != thread.ID {
			t.Fatalf("concerns = %+v, %v", concerns, err)
		}
		return concerns[0]
	}
	item := concern()
	if item.Outcome != outcome {
		t.Fatalf("concern = %+v, want outcome %q", item, outcome)
	}
	dir := filepath.Join(home, "work", item.ID)
	doc := "work/" + item.ID + "/changes.md"

	// The first slot writes the first part in the concern's work directory.
	before := provider.requestCount()
	provider.add(
		toolCallChunk("load_files", "tool_search", map[string]string{"query": "select:files"}),
		toolCallChunk("write", "files", map[string]string{"action": "write", "path": doc, "content": "# X 的版本变化\n\n## v1.4\n- 移除旧配置项\n"}),
		toolCallChunk("advance_1", "concern", map[string]any{"action": "active", "id": item.ID, "revision": item.Revision, "title": item.Title, "reason": "整理完 v1.4，下一步是 v1.5", "source_id": "__input_source__"}),
		textReply("v1.4 整理完了，写在 changes.md 里。"),
	)
	h.clock.Set(time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	latest = waitNewRun(t, c, thread.ID, latest).RunID
	if text := requestText(provider.request(before)); !strings.Contains(text, "Advance concern "+item.ID+" rev 1 [active]: 整理 X 近几个版本的变化\nOutcome: "+outcome+"\nLatest reason: owner 交代整理成文档\nReview at: not set\nWork directory: ~/work/"+item.ID+"/ on your workstation") {
		t.Fatalf("first advance input:\n%s", text)
	}
	if written, err := os.ReadFile(filepath.Join(dir, "changes.md")); err != nil || !strings.Contains(string(written), "## v1.4") {
		t.Fatalf("work file after the first advance = %q, %v", written, err)
	}
	item = concern()
	if item.Revision != 2 || item.State != "active" || item.Outcome != outcome {
		t.Fatalf("concern after the first advance = %+v", item)
	}

	// The second slot's command waits for approval, then the advance delivers.
	provider.add(
		toolCallChunk("load_shell", "tool_search", map[string]string{"query": "select:shell"}),
		toolCallChunk("append", "shell", map[string]string{"command": "printf '\\n## v1.5\\n- 配置改为 YAML，升级时先运行 x migrate\\n' >> ~/" + doc}),
		toolCallChunk("deliver", "send_file", map[string]string{"path": doc, "name": "X 版本变化.md"}),
		toolCallChunk("push", "notify_owner", map[string]string{"title": "X 的版本变化整理好了", "body": "文档已发在对话里"}),
		toolCallChunk("self_resolve", "concern", map[string]any{"action": "resolved", "id": item.ID, "revision": item.Revision, "title": item.Title, "reason": "文档已交付", "source_id": "__notify_source__"}),
		toolCallChunk("wait", "concern", map[string]any{"action": "waiting", "id": item.ID, "revision": item.Revision, "title": item.Title, "reason": "X 版本变化.md 已发在本对话里，等 owner 确认", "source_id": "__notify_source__", "review_at": "2026-10-15T10:00:00+08:00"}),
		textReply("整理好了，文档发在上面。"),
	)
	h.clock.Set(time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	advance, err := c.store.LoadLatestRunForSession(ctx, thread.ID)
	if err != nil || advance == nil || advance.RunID == latest {
		t.Fatalf("second advance = %+v, %v", advance, err)
	}
	waitRunStatus(t, c, advance.RunID, "interrupted")
	actions, err := c.pendingAction.List(ctx, 10)
	if err != nil || len(actions) != 1 || actions[0].RunID != advance.RunID {
		t.Fatalf("pending actions = %+v, %v", actions, err)
	}
	if written, err := os.ReadFile(filepath.Join(dir, "changes.md")); err != nil || strings.Contains(string(written), "## v1.5") {
		t.Fatalf("the command ran before approval: %q, %v", written, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.fcm.sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	decideOverHTTP(t, c, actions[0].ActionID, "accept")
	waitRunStatus(t, c, advance.RunID, "completed")

	if written, err := os.ReadFile(filepath.Join(dir, "changes.md")); err != nil || !strings.Contains(string(written), "## v1.4") || !strings.Contains(string(written), "## v1.5") {
		t.Fatalf("work file after the second advance = %q, %v", written, err)
	}
	if refused := toolResult(t, provider.request(provider.requestCount()-2), "self_resolve"); !strings.Contains(refused, "only by the owner's confirmation") {
		t.Fatalf("the agent resolved its own delivery: %q", refused)
	}
	item = concern()
	if item.Revision != 3 || item.State != "waiting" || item.Outcome != outcome {
		t.Fatalf("concern after delivery = %+v", item)
	}
	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	var delivered []api.ThreadFileDTO
	for _, msg := range listed.Items {
		if msg.RunID == advance.RunID {
			delivered = append(delivered, msg.Files...)
		}
	}
	if len(delivered) != 1 || delivered[0].Name != "X 版本变化.md" || delivered[0].Origin != "agent" {
		t.Fatalf("files delivered in the owner's conversation = %+v", delivered)
	}
	var approvals, pushes int
	for _, sent := range h.fcm.sent() {
		data, _ := sent["data"].(map[string]any)
		if data["thread_id"] != thread.ID || data["run_id"] != advance.RunID {
			t.Fatalf("push outside the advance: %v", sent)
		}
		if data["type"] == "pending_action" {
			approvals++
		} else {
			pushes++
		}
	}
	if approvals != 1 || pushes != 1 {
		t.Fatalf("pushes: %d for approval and %d for the delivery, want one each", approvals, pushes)
	}

	// A waiting concern is left alone until its review time.
	calls := provider.requestCount()
	h.clock.Set(time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.requestCount() != calls {
		t.Fatal("a slot advanced the delivered concern before its review time")
	}

	// The owner confirms in the conversation.
	provider.add(
		toolCallChunk("confirm", "concern", map[string]any{"action": "resolved", "id": item.ID, "revision": item.Revision, "title": item.Title, "reason": "owner 确认了文档", "source_id": "__input_source__"}),
		textReply("好的，这件事结了。"),
	)
	if _, err := c.runs.CreateRun(ctx, thread.ID, "", "看过了，可以", nil); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, advance.RunID)
	if item = concern(); item.State != "resolved" || item.Revision != 4 {
		t.Fatalf("concern after the owner's confirmation = %+v", item)
	}
	if open, err := c.store.ListConcerns(ctx, true); err != nil || len(open) != 0 {
		t.Fatalf("open concerns after the owner's confirmation = %+v, %v", open, err)
	}
	if got := len(h.fcm.sent()); got != 2 {
		t.Fatalf("fcm messages after the confirmation = %d, want 2", got)
	}
}

// requestText joins the text of every message in a model request.
func requestText(request map[string]any) string {
	var b strings.Builder
	for _, msg := range requestMessages(request) {
		if msg["content"] != nil {
			b.WriteString(chatContentText(msg["content"]))
			b.WriteString("\n")
		}
	}
	return b.String()
}
