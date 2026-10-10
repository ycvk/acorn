package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/store"
)

var presenceTestNow = time.Date(2026, 10, 2, 4, 0, 0, 0, time.UTC)

type unusedMemoryModel struct{}

func (unusedMemoryModel) GenerateMemory(context.Context, string, string, int) (memory.Generation, error) {
	return memory.Generation{}, errors.New("unexpected generation in synchronous tool test")
}

type unusedEmbedding struct{ embedding.Embedder }

func testPresenceDeps(t *testing.T, bridge core.ToolCallContextBridge) (PresenceToolDeps, *store.Store) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := memory.New(memory.Config{Store: db, Model: unusedMemoryModel{}, ModelName: "fixture", Embedder: unusedEmbedding{}, Index: core.MemoryIndex{Model: "fixture", Dimensions: 1}, Count: func(_ context.Context, s string) (int, error) { return len(s), nil }, Clock: func() time.Time { return presenceTestNow }, Location: loc, BatchTokens: 8192, ContextTokens: 8192, HistoryTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	return PresenceToolDeps{Store: db, Memory: engine, Context: bridge, Clock: func() time.Time { return presenceTestNow }, Location: loc, ForgetBarrier: func(context.Context, core.MemoryExclusion, string) error { return nil }}, db
}
func runPresenceTool(t *testing.T, tool einotool.BaseTool, args string) (string, error) {
	t.Helper()
	return tool.(einotool.InvokableTool).InvokableRun(context.Background(), args)
}
func newPresenceToolsForTest(t *testing.T) (*store.Store, map[string]einotool.BaseTool) {
	t.Helper()
	deps, db := testPresenceDeps(t, fixedArtifactContext{runID: "run_1", sessionID: "thread_1", callID: "call_1"})
	tools := map[string]einotool.BaseTool{}
	keep, err := buildMemoryWriteTool("keep", "keep", deps)
	if err != nil {
		t.Fatal(err)
	}
	tools["keep"] = keep
	think, err := buildMemoryWriteTool("think", "think", deps)
	if err != nil {
		t.Fatal(err)
	}
	tools["think"] = think
	for name, build := range map[string]func(PresenceToolDeps) (einotool.BaseTool, error){"schedule_wake": buildScheduleWakeTool, "settle": buildSettleTool, "memory_correct": buildMemoryCorrectTool, "memory_forget": buildMemoryForgetTool} {
		tool, err := build(deps)
		if err != nil {
			t.Fatal(err)
		}
		tools[name] = tool
	}
	return db, tools
}
func TestKeepAndThinkRequireTraceableCurrentSource(t *testing.T) {
	db, tools := newPresenceToolsForTest(t)
	ctx := context.Background()
	_, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner-1", Kind: "fixture", ObjectID: "owner-1", Version: "1", Speaker: "owner", RunID: "run_1", SessionID: "thread_1", Body: "周末别打扰我", RecordedAt: presenceTestNow})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, tools["keep"], `{"content":"周末别打扰我","source_id":"owner-1","quote":"周末别打扰我"}`)
	if err != nil {
		t.Fatal(err)
	}
	var fact core.MemoryRecord
	if err = json.Unmarshal([]byte(out), &fact); err != nil {
		t.Fatal(err)
	}
	if fact.Kind != "fact" || fact.Basis != "direct" || !fact.Pinned {
		t.Fatalf("fact=%+v", fact)
	}
	out, err = runPresenceTool(t, tools["think"], `{"content":"owner 可能需要更多安静时间","source_id":"owner-1","quote":"周末别打扰我"}`)
	if err != nil {
		t.Fatal(err)
	}
	var thought core.MemoryRecord
	if err = json.Unmarshal([]byte(out), &thought); err != nil {
		t.Fatal(err)
	}
	if thought.Kind != "thought" || thought.State != "open" || thought.Basis != "inferred" {
		t.Fatalf("thought=%+v", thought)
	}
	if _, err = runPresenceTool(t, tools["keep"], `{"content":"owner 喜欢蓝色","source_id":"owner-1","quote":"喜欢蓝色"}`); err == nil {
		t.Fatal("unsupported quotation accepted")
	}
	if _, err = runPresenceTool(t, tools["keep"], `{"content":" ","source_id":"owner-1","quote":"周末别打扰我"}`); err == nil {
		t.Fatal("empty fact accepted")
	}
}
func TestSettleRequiresOutcomeSource(t *testing.T) {
	db, tools := newPresenceToolsForTest(t)
	ctx := context.Background()
	if _, err := runPresenceTool(t, tools["schedule_wake"], `{"task":"提醒","in":"1h"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, tools["settle"], `{"id":1,"action":"done"}`); err == nil {
		t.Fatal("unsourced completion accepted")
	}
	_, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner-done", Kind: "fixture", ObjectID: "owner-done", Version: "1", Speaker: "owner", RunID: "run_1", Body: "取消提醒", RecordedAt: presenceTestNow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runPresenceTool(t, tools["settle"], `{"id":1,"action":"cancel","source_id":"owner-done"}`); err != nil {
		t.Fatal(err)
	}
	item, err := db.LoadCommitment(ctx, 1)
	if err != nil || item.State != "cancelled" {
		t.Fatalf("commitment=%+v err=%v", item, err)
	}
}
func TestScheduleWakeResolvesOwnerTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want time.Time
	}{
		{"in days", `{"task":"提醒看 X","in":"3d"}`, presenceTestNow.Add(72 * time.Hour)},
		{"in duration", `{"task":"t","in":"90m"}`, presenceTestNow.Add(90 * time.Minute)},
		{"local at", `{"task":"t","at":"2026-10-03 09:00"}`, time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)},
		{"rfc3339", `{"task":"t","at":"2026-10-03T09:00:00Z"}`, time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)},
		{"recurrence only", `{"task":"t","recurrence":"0 8 * * *"}`, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, tools := newPresenceToolsForTest(t)
			if _, err := runPresenceTool(t, tools["schedule_wake"], tc.args); err != nil {
				t.Fatalf("schedule_wake: %v", err)
			}
			item, err := store.LoadCommitment(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if item.State != "scheduled" || item.SessionID != "thread_1" || !item.WakeAt.Equal(tc.want) {
				t.Fatalf("commitment = %+v, want wake %v", item, tc.want)
			}
		})
	}
	for _, args := range []string{
		`{"task":"t"}`,
		`{"task":"t","in":"2h","at":"2026-10-03 09:00"}`,
		`{"task":"t","at":"2026-10-01 09:00"}`,
		`{"task":"t","in":"-1h"}`,
		`{"task":"t","in":"soon"}`,
		`{"task":"t","recurrence":"* *"}`,
		`{"task":"","in":"1h"}`,
	} {
		_, tools := newPresenceToolsForTest(t)
		if _, err := runPresenceTool(t, tools["schedule_wake"], args); err == nil {
			t.Fatalf("schedule_wake %s should fail", args)
		}
	}
}

type fakeNotifier struct {
	got    []core.Notification
	status core.NotificationStatus
	err    error
}

func (f *fakeNotifier) Notify(_ context.Context, n core.Notification) (core.Notification, error) {
	f.got = append(f.got, n)
	if f.err != nil {
		return core.Notification{}, f.err
	}
	n.Status = f.status
	n.SendAfter = presenceTestNow.Add(10 * time.Hour)
	return n, nil
}

func TestNotifyOwnerAttributesConversationAndReportsQueue(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	notifier := &fakeNotifier{status: core.NotificationQueued}
	tool, err := buildNotifyOwnerTool(NotifyToolDeps{Notifier: notifier, Context: fixedArtifactContext{runID: "run_1", sessionID: "thread_1"}, Location: loc})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, tool, `{"title":"提醒","body":"该看 X 了"}`)
	if err != nil {
		t.Fatalf("notify_owner: %v", err)
	}
	if !strings.Contains(out, `"status":"queued"`) || !strings.Contains(out, "2026-10-02 Fri 22:00") {
		t.Fatalf("output = %s", out)
	}
	if got := notifier.got[0]; got.ThreadID != "thread_1" || got.RunID != "run_1" || got.Title != "提醒" {
		t.Fatalf("notification = %+v", got)
	}
	notifier.err = errors.New("no device has registered for push notifications")
	if _, err := runPresenceTool(t, tool, `{"title":"a","body":"b"}`); err == nil || !strings.Contains(err.Error(), "no device") {
		t.Fatalf("notifier error must surface: %v", err)
	}
	if _, err := runPresenceTool(t, tool, `{"title":"","body":"b"}`); err == nil {
		t.Fatal("empty title must fail")
	}
}
