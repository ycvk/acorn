package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/core"
)

// memPresenceStore is an in-memory core.PresenceStore that also records
// snapshots and events.
type memPresenceStore struct {
	mu        sync.Mutex
	items     []core.MemoryItem
	snapshots map[string]string
	events    []string
}

func newMemPresenceStore() *memPresenceStore {
	return &memPresenceStore{snapshots: map[string]string{}}
}

func (s *memPresenceStore) AddMemoryItem(_ context.Context, item core.MemoryItem) (core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item.ID = int64(len(s.items) + 1)
	s.items = append(s.items, item)
	return item, nil
}

func (s *memPresenceStore) ListMemoryItems(_ context.Context, statuses []core.MemoryStatus) ([]core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.MemoryItem
	for _, item := range s.items {
		for _, status := range statuses {
			if item.Status == status {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func (s *memPresenceStore) LoadMemoryItem(_ context.Context, id int64) (*core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.items[id-1]
	return &item, nil
}

func (s *memPresenceStore) UpdateMemoryItem(_ context.Context, item core.MemoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[item.ID-1] = item
	return nil
}

func (s *memPresenceStore) ListDueCommitments(context.Context, time.Time) ([]core.MemoryItem, error) {
	return nil, nil
}

func (s *memPresenceStore) ClaimDueCommitment(context.Context, int64, time.Time) error { return nil }

func (s *memPresenceStore) SearchExperience(context.Context, string, int) ([]core.ExperienceHit, error) {
	return nil, nil
}

func (s *memPresenceStore) CountWakesSince(context.Context, time.Time) (int, error) { return 0, nil }

func (s *memPresenceStore) SaveContextSnapshot(_ context.Context, hash, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[hash] = content
	return nil
}

func (s *memPresenceStore) AppendEvent(_ context.Context, _ string, kind string, _ any) (core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, kind)
	return core.EventRecord{}, nil
}

func (s *memPresenceStore) snapshotEvents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, kind := range s.events {
		if kind == "presence.snapshot" {
			n++
		}
	}
	return n
}

var presenceNow = time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)

func newTestPresenceMiddleware(t *testing.T, store *memPresenceStore) *presenceMiddleware {
	t.Helper()
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("token counter: %v", err)
	}
	return &presenceMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		store:                        store,
		events:                       store,
		clock:                        func() time.Time { return presenceNow },
		location:                     time.UTC,
		maxTokens:                    2000,
		counter:                      counter,
		runID:                        "run_1",
	}
}

// thinkTool writes a thought so the present changes between model calls.
type thinkTool struct{ store *memPresenceStore }

func (thinkTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "think", Desc: "note a thought"}, nil
}

func (t thinkTool) InvokableRun(ctx context.Context, args string, _ ...einotool.Option) (string, error) {
	_, err := t.store.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemoryThought, Status: core.MemoryActive, Content: "noted " + args, CreatedAt: presenceNow})
	return "ok", err
}

func countPresence(messages []*schema.Message) int {
	n := 0
	for _, msg := range messages {
		if strings.HasPrefix(msg.Content, "<presence>") {
			n++
		}
	}
	return n
}

func TestPresenceIsAppendedPerCallAndKeptOutOfState(t *testing.T) {
	store := newMemPresenceStore()
	if _, err := store.AddMemoryItem(context.Background(), core.MemoryItem{Kind: core.MemorySaid, Status: core.MemoryActive, Content: "我喜欢早上看新闻", CreatedAt: presenceNow}); err != nil {
		t.Fatal(err)
	}
	model := &scriptedModel{replies: []*schema.Message{
		toolCallReply("call_1", "think", `{"x":1}`),
		schema.AssistantMessage("done", nil),
	}}
	ctx := context.Background()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "presence_test",
		Description: "presence test agent",
		Instruction: "You are Acorn.",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               []einotool.BaseTool{thinkTool{store: store}},
			ExecuteSequentially: true,
		}},
		Handlers:      []adk.ChatModelAgentMiddleware{newTestPresenceMiddleware(t, store)},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	wakeCtx := core.WithWake(ctx, "commitment #9: 提醒 owner 看 X")
	if _, err := drainEvents(t, runner.Run(wakeCtx, []adk.Message{schema.UserMessage("hi")})); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(model.inputs) != 2 {
		t.Fatalf("model calls = %d, want 2", len(model.inputs))
	}
	for i, input := range model.inputs {
		last := input[len(input)-1]
		if last.Role != schema.System || !strings.HasPrefix(last.Content, "<presence>") {
			t.Fatalf("call %d: last message = %s %q, want the present", i, last.Role, last.Content)
		}
		if got := countPresence(input); got != 1 {
			t.Fatalf("call %d carries %d presence blocks; earlier ones leaked into state", i, got)
		}
		if !strings.Contains(last.Content, "Woken by: commitment #9") || !strings.Contains(last.Content, "我喜欢早上看新闻") {
			t.Fatalf("call %d present = %q", i, last.Content)
		}
	}
	if !strings.Contains(model.inputs[1][len(model.inputs[1])-1].Content, `noted {"x":1}`) {
		t.Fatal("second call must see the thought written by the tool")
	}
	if got := store.snapshotEvents(); got != 2 || len(store.snapshots) != 2 {
		t.Fatalf("snapshot events = %d, snapshots = %d; want one per distinct present", got, len(store.snapshots))
	}
}

func TestPresenceSnapshotOnlyWhenRenderChanges(t *testing.T) {
	store := newMemPresenceStore()
	mw := newTestPresenceMiddleware(t, store)
	input := []*schema.Message{schema.UserMessage("hi")}
	for range 3 {
		out, err := mw.withPresence(context.Background(), input)
		if err != nil {
			t.Fatalf("with presence: %v", err)
		}
		if len(out) != 2 || len(input) != 1 {
			t.Fatalf("presence must be appended to a copy: out=%d input=%d", len(out), len(input))
		}
		if !strings.Contains(out[1].Content, "Woken by: owner message") {
			t.Fatalf("default wake missing: %q", out[1].Content)
		}
	}
	if got := store.snapshotEvents(); got != 1 {
		t.Fatalf("snapshot events = %d, want 1 for an unchanged present", got)
	}
}

func TestPresenceDecaysBeforeRendering(t *testing.T) {
	store := newMemPresenceStore()
	ctx := context.Background()
	if _, err := store.AddMemoryItem(ctx, core.MemoryItem{Kind: core.MemoryThought, Status: core.MemoryResting, Content: "old idea", ExpiresAt: presenceNow.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	out, err := newTestPresenceMiddleware(t, store).withPresence(ctx, nil)
	if err != nil {
		t.Fatalf("with presence: %v", err)
	}
	if strings.Contains(out[0].Content, "old idea") {
		t.Fatalf("sunk item rendered: %q", out[0].Content)
	}
	if item, _ := store.LoadMemoryItem(ctx, 1); item.Status != core.MemorySunk {
		t.Fatalf("item status = %s, want sunk persisted", item.Status)
	}
}
