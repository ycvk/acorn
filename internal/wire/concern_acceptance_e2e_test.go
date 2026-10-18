package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
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
	if !strings.Contains(text, "[wander 2026-10-13 10:00] Advance concern "+a.ID+" rev 2 [active]: 跟进 A 项目的发布. Latest reason: 读完 v1.4 的发布说明") || !strings.Contains(text, "A 第一次推进：读完了 v1.4 的发布说明。") {
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
