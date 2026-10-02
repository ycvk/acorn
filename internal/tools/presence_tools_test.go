package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// fakePresenceStore is an in-memory core.PresenceStore.
type fakePresenceStore struct {
	mu    sync.Mutex
	next  int64
	items map[int64]core.MemoryItem
	hits  []core.ExperienceHit
	query string
	limit int
}

func newFakePresenceStore() *fakePresenceStore {
	return &fakePresenceStore{items: map[int64]core.MemoryItem{}}
}

func (s *fakePresenceStore) AddMemoryItem(_ context.Context, item core.MemoryItem) (core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	item.ID = s.next
	item.CreatedAt = time.Now()
	s.items[item.ID] = item
	return item, nil
}

func (s *fakePresenceStore) ListMemoryItems(_ context.Context, statuses []core.MemoryStatus) ([]core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.MemoryItem
	for id := int64(1); id <= s.next; id++ {
		item, ok := s.items[id]
		if !ok {
			continue
		}
		for _, status := range statuses {
			if item.Status == status {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func (s *fakePresenceStore) LoadMemoryItem(_ context.Context, id int64) (*core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", core.ErrMemoryItemNotFound, id)
	}
	return &item, nil
}

func (s *fakePresenceStore) UpdateMemoryItem(_ context.Context, item core.MemoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[item.ID]; !ok {
		return fmt.Errorf("%w: %d", core.ErrMemoryItemNotFound, item.ID)
	}
	s.items[item.ID] = item
	return nil
}

func (s *fakePresenceStore) ListDueCommitments(context.Context, time.Time) ([]core.MemoryItem, error) {
	return nil, nil
}

func (s *fakePresenceStore) ClaimDueCommitment(context.Context, int64, time.Time) error { return nil }

func (s *fakePresenceStore) SearchExperience(_ context.Context, query string, limit int) ([]core.ExperienceHit, error) {
	s.query, s.limit = query, limit
	return s.hits, nil
}

func (s *fakePresenceStore) CountWakesSince(context.Context, time.Time) (int, error) { return 0, nil }

func (s *fakePresenceStore) SaveContextSnapshot(context.Context, string, string) error { return nil }

var presenceTestNow = time.Date(2026, 10, 2, 4, 0, 0, 0, time.UTC) // 12:00 in Shanghai

func testPresenceDeps(store core.PresenceStore, bridge core.ToolCallContextBridge) PresenceToolDeps {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return PresenceToolDeps{Store: store, Context: bridge, Clock: func() time.Time { return presenceTestNow }, Location: loc}
}

func runPresenceTool(t *testing.T, tool einotool.BaseTool, args string) (string, error) {
	t.Helper()
	invokable, ok := tool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("tool is not invokable")
	}
	return invokable.InvokableRun(context.Background(), args)
}

func newPresenceToolsForTest(t *testing.T) (*fakePresenceStore, map[string]einotool.BaseTool) {
	t.Helper()
	store := newFakePresenceStore()
	deps := testPresenceDeps(store, fixedArtifactContext{runID: "run_1", sessionID: "thread_1", callID: "call_1"})
	keep, err := buildMemoryWriteTool("keep", "keep", core.MemorySaid, deps)
	if err != nil {
		t.Fatal(err)
	}
	think, err := buildMemoryWriteTool("think", "think", core.MemoryThought, deps)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := buildScheduleWakeTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	settle, err := buildSettleTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	recall, err := buildRecallTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	return store, map[string]einotool.BaseTool{"keep": keep, "think": think, "schedule_wake": schedule, "settle": settle, "recall": recall}
}

func TestKeepAndThinkStoreAttributedItems(t *testing.T) {
	store, tools := newPresenceToolsForTest(t)
	out, err := runPresenceTool(t, tools["keep"], `{"content":"周末别打扰我"}`)
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if !strings.Contains(out, `"kind":"said"`) || !strings.Contains(out, "2026-10-09") {
		t.Fatalf("keep output = %s", out)
	}
	if _, err := runPresenceTool(t, tools["think"], `{"content":"owner 在赶 deadline"}`); err != nil {
		t.Fatalf("think: %v", err)
	}
	said, thought := store.items[1], store.items[2]
	if said.Kind != core.MemorySaid || said.SessionID != "thread_1" || said.SourceRunID != "run_1" || said.Status != core.MemoryActive {
		t.Fatalf("said = %+v", said)
	}
	if thought.Kind != core.MemoryThought || !thought.ExpiresAt.Equal(presenceTestNow.Add(48*time.Hour)) {
		t.Fatalf("thought = %+v", thought)
	}
	if _, err := runPresenceTool(t, tools["keep"], `{"content":"  "}`); err == nil {
		t.Fatal("empty keep must fail")
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
			item := store.items[1]
			if item.Kind != core.MemoryCommitment || item.Status != core.MemoryActive || item.SessionID != "thread_1" || !item.WakeAt.Equal(tc.want) {
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

func TestSettleActions(t *testing.T) {
	store, tools := newPresenceToolsForTest(t)
	mustRun := func(name, args string) string {
		t.Helper()
		out, err := runPresenceTool(t, tools[name], args)
		if err != nil {
			t.Fatalf("%s %s: %v", name, args, err)
		}
		return out
	}
	mustRun("keep", `{"content":"我喜欢早上看新闻"}`)           // #1
	mustRun("think", `{"content":"maybe"}`)             // #2
	mustRun("schedule_wake", `{"task":"提醒","in":"1h"}`) // #3
	out := mustRun("settle", `{"id":1,"action":"internalize","as":"tendency","content":"早上看新闻"}`)
	var settled SettleOutput
	if err := json.Unmarshal([]byte(out), &settled); err != nil || settled.NewID != 4 || settled.Status != "internalized" {
		t.Fatalf("internalize = %s err=%v", out, err)
	}
	if store.items[4].Kind != core.MemoryTendency || store.items[4].Status != core.MemoryActive {
		t.Fatalf("tendency = %+v", store.items[4])
	}
	stale := store.items[2]
	stale.Status, stale.ExpiresAt = core.MemoryResting, presenceTestNow
	store.items[2] = stale
	mustRun("settle", `{"id":2,"action":"renew"}`)
	if got := store.items[2]; got.Status != core.MemoryActive || !got.ExpiresAt.Equal(presenceTestNow.Add(48*time.Hour)) {
		t.Fatalf("renewed = %+v", got)
	}
	for _, args := range []string{
		`{"id":3,"action":"done"}`,  // not woken yet
		`{"id":3,"action":"renew"}`, // commitments are not renewed
		`{"id":2,"action":"internalize","as":"said","content":"x"}`,
		`{"id":1,"action":"release"}`, // already internalized
		`{"id":99,"action":"release"}`,
		`{"id":2,"action":"forget"}`,
	} {
		if _, err := runPresenceTool(t, tools["settle"], args); err == nil {
			t.Fatalf("settle %s should fail", args)
		}
	}
	woken := store.items[3]
	woken.Status = core.MemoryWoken
	store.items[3] = woken
	mustRun("settle", `{"id":3,"action":"done"}`)
	if store.items[3].Status != core.MemorySettled {
		t.Fatalf("commitment = %+v", store.items[3])
	}
	mustRun("settle", `{"id":2,"action":"release"}`)
	if store.items[2].Status != core.MemoryReleased {
		t.Fatalf("released = %+v", store.items[2])
	}
}

func TestRecallFormatsHitsInOwnerTime(t *testing.T) {
	store, tools := newPresenceToolsForTest(t)
	store.hits = []core.ExperienceHit{{Source: "run", RunID: "run_9", Snippet: "看论文", CreatedAt: presenceTestNow}}
	out, err := runPresenceTool(t, tools["recall"], `{"query":"论文","limit":500}`)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if store.query != "论文" || store.limit != 50 {
		t.Fatalf("store got query=%q limit=%d", store.query, store.limit)
	}
	if !strings.Contains(out, `"run_id":"run_9"`) || !strings.Contains(out, "2026-10-02 Fri 12:00") {
		t.Fatalf("recall output = %s", out)
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
