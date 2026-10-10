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

// memPresenceStore records the deterministic present for middleware tests.
type memPresenceStore struct {
	core.PhoneNotificationStore
	core.MemoryStore
	core.CommitmentStore
	mu        sync.Mutex
	items     []core.MemoryRecord
	snapshots map[string]string
	events    []string
}

func newMemPresenceStore() *memPresenceStore {
	return &memPresenceStore{snapshots: map[string]string{}}
}

func (s *memPresenceStore) ListMemoryRecords(context.Context, core.MemoryQuery) ([]core.MemoryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]core.MemoryRecord(nil), s.items...), nil
}
func (s *memPresenceStore) ListConcerns(context.Context, bool) ([]core.MemoryConcern, error) {
	return nil, nil
}
func (s *memPresenceStore) ListCommitments(context.Context, bool) ([]core.Commitment, error) {
	return nil, nil
}
func (s *memPresenceStore) ListDueOccurrences(context.Context) ([]core.CommitmentOccurrence, error) {
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
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		store:                             store,
		memory:                            store,
		commitments:                       store,
		phones:                            store,
		events:                            store,
		clock:                             func() time.Time { return presenceNow },
		location:                          time.UTC,
		maxTokens:                         2000,
		inputLimit:                        200000,
		counter:                           counter,
		runID:                             "run_1",
	}
}

// thinkTool writes a thought so the present changes between model calls.
type thinkTool struct{ store *memPresenceStore }

func (thinkTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "think", Desc: "note a thought"}, nil
}

func (t thinkTool) InvokableRun(ctx context.Context, args string, _ ...einotool.Option) (string, error) {
	t.store.mu.Lock()
	defer t.store.mu.Unlock()
	t.store.items = append(t.store.items, core.MemoryRecord{ID: "new-thought", Kind: "thought", State: "open", Content: "noted " + args, UpdatedAt: presenceNow})
	return "ok", nil
}

func countPresence(messages []*schema.AgenticMessage) int {
	n := 0
	for _, msg := range messages {
		if strings.HasPrefix(messageText(msg), "<presence>") {
			n++
		}
	}
	return n
}

func TestPresenceIsAppendedPerCallAndKeptOutOfState(t *testing.T) {
	store := newMemPresenceStore()
	store.items = append(store.items, core.MemoryRecord{ID: "morning", Kind: "thought", State: "open", Content: "我喜欢早上看新闻", UpdatedAt: presenceNow})
	model := &scriptedModel{replies: []*schema.AgenticMessage{
		toolCallReply("call_1", "think", `{"x":1}`),
		assistantMessage("done", nil),
	}}
	ctx := context.Background()
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "presence_test",
		Description: "presence test agent",
		Instruction: "You are Acorn.",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               []einotool.BaseTool{thinkTool{store: store}},
			ExecuteSequentially: true,
		}},
		Handlers:      []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{newTestPresenceMiddleware(t, store)},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent})
	wakeCtx := core.WithWake(ctx, "commitment #9: 提醒 owner 看 X")
	if _, err := drainEvents(t, runner.Run(wakeCtx, []adk.AgenticMessage{schema.UserAgenticMessage("hi")})); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(model.inputs) != 2 {
		t.Fatalf("model calls = %d, want 2", len(model.inputs))
	}
	for i, input := range model.inputs {
		last := input[len(input)-1]
		if last.Role != schema.AgenticRoleTypeSystem || !strings.HasPrefix(messageText(last), "<presence>") {
			t.Fatalf("call %d: last message = %s %q, want the present", i, last.Role, messageText(last))
		}
		if got := countPresence(input); got != 1 {
			t.Fatalf("call %d carries %d presence blocks; earlier ones leaked into state", i, got)
		}
		if !strings.Contains(messageText(last), "Woken by: commitment #9") || !strings.Contains(messageText(last), "我喜欢早上看新闻") {
			t.Fatalf("call %d present = %q", i, messageText(last))
		}
	}
	if !strings.Contains(messageText(model.inputs[1][len(model.inputs[1])-1]), `noted {"x":1}`) {
		t.Fatal("second call must see the thought written by the tool")
	}
	if got := store.snapshotEvents(); got != 2 || len(store.snapshots) != 2 {
		t.Fatalf("snapshot events = %d, snapshots = %d; want one per distinct present", got, len(store.snapshots))
	}
}

func TestPresenceSnapshotOnlyWhenRenderChanges(t *testing.T) {
	store := newMemPresenceStore()
	mw := newTestPresenceMiddleware(t, store)
	input := []*schema.AgenticMessage{schema.UserAgenticMessage("hi")}
	for range 3 {
		out, err := mw.withPresence(context.Background(), input)
		if err != nil {
			t.Fatalf("with presence: %v", err)
		}
		if len(out) != 2 || len(input) != 1 {
			t.Fatalf("presence must be appended to a copy: out=%d input=%d", len(out), len(input))
		}
		if !strings.Contains(messageText(out[1]), "Woken by: owner message") {
			t.Fatalf("default wake missing: %q", messageText(out[1]))
		}
	}
	if got := store.snapshotEvents(); got != 1 {
		t.Fatalf("snapshot events = %d, want 1 for an unchanged present", got)
	}
}

func TestPresenceExcludesResolvedThoughts(t *testing.T) {
	store := newMemPresenceStore()
	store.items = []core.MemoryRecord{{Kind: "thought", State: "resolved", Content: "old idea"}}
	out, err := newTestPresenceMiddleware(t, store).withPresence(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(messageText(out[0]), "old idea") {
		t.Fatal("resolved thought is active")
	}
	if store.items[0].State != "resolved" {
		t.Fatal("render mutated thought state")
	}
}

func (s *memPresenceStore) SumAutonomousTokensSince(context.Context, time.Time) (int, error) {
	return 0, nil
}
func (s *memPresenceStore) UsageReport(context.Context, time.Time) (core.UsageReport, error) {
	return core.UsageReport{}, nil
}

func (s *memPresenceStore) ListPhoneNotifications(context.Context, time.Time, time.Time, int) (core.PhoneNotificationPage, error) {
	return core.PhoneNotificationPage{}, nil
}
