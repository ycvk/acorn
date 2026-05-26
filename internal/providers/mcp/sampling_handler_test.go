package mcpprovider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/stream"
)

// mockSamplingExecutor is a test double for SamplingExecutor.
type mockSamplingExecutor struct {
	output string
	err    error
	calls  int
	mu     sync.Mutex
}

func (m *mockSamplingExecutor) ExecuteMessages(_ context.Context, _ []*schema.Message) (string, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	return m.output, nil
}

func (m *mockSamplingExecutor) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockSamplingEventStore is a test double for samplingEventStore.
type mockSamplingEventStore struct {
	events []struct {
		runID   string
		kind    string
		payload stream.SamplingPayload
	}
	mu sync.Mutex
}

func (m *mockSamplingEventStore) AppendEventContext(_ context.Context, runID, kind string, payload any) (events.EventRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, _ := payload.(stream.SamplingPayload)
	m.events = append(m.events, struct {
		runID   string
		kind    string
		payload stream.SamplingPayload
	}{runID: runID, kind: kind, payload: p})
	return events.EventRecord{}, nil
}

func (m *mockSamplingEventStore) getEvents() []struct {
	runID   string
	kind    string
	payload stream.SamplingPayload
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]struct {
		runID   string
		kind    string
		payload stream.SamplingPayload
	}{}, m.events...)
}

// --- convertSamplingMessages tests ---

func TestConvertSamplingMessagesUser(t *testing.T) {
	msgs := []*mcp.SamplingMessage{
		{Role: "user", Content: &mcp.TextContent{Text: "hello"}},
	}
	result := convertSamplingMessages(msgs, "")
	if got, want := len(result), 1; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	if got, want := string(result[0].Role), "user"; got != want {
		t.Fatalf("role = %q, want %q", got, want)
	}
	if got, want := result[0].Content, "hello"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestConvertSamplingMessagesAssistant(t *testing.T) {
	msgs := []*mcp.SamplingMessage{
		{Role: "assistant", Content: &mcp.TextContent{Text: "response"}},
	}
	result := convertSamplingMessages(msgs, "")
	if got, want := len(result), 1; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	if got, want := string(result[0].Role), "assistant"; got != want {
		t.Fatalf("role = %q, want %q", got, want)
	}
}

func TestConvertSamplingMessagesWithSystemPrompt(t *testing.T) {
	msgs := []*mcp.SamplingMessage{
		{Role: "user", Content: &mcp.TextContent{Text: "hello"}},
	}
	result := convertSamplingMessages(msgs, "you are a helper")
	if got, want := len(result), 2; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	// First message should be system
	if got, want := string(result[0].Role), "system"; got != want {
		t.Fatalf("first role = %q, want %q", got, want)
	}
	if got, want := result[0].Content, "you are a helper"; got != want {
		t.Fatalf("system content = %q, want %q", got, want)
	}
	// Second message should be user
	if got, want := string(result[1].Role), "user"; got != want {
		t.Fatalf("second role = %q, want %q", got, want)
	}
}

func TestConvertSamplingMessagesSkipsNonText(t *testing.T) {
	msgs := []*mcp.SamplingMessage{
		{Role: "user", Content: &mcp.ImageContent{}},
		{Role: "user", Content: &mcp.TextContent{Text: "text only"}},
	}
	result := convertSamplingMessages(msgs, "")
	if got, want := len(result), 1; got != want {
		t.Fatalf("len = %d, want %d (non-text content skipped)", got, want)
	}
	if got, want := result[0].Content, "text only"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// --- SamplingHandler tests ---

func newTestSamplingHandler(mgr *Manager) *SamplingHandler {
	h := newSamplingHandler(mgr)
	h.store = &mockSamplingEventStore{}
	return h
}

func TestHandleCreateMessageSucceeds(t *testing.T) {
	mgr := &Manager{samplingDepth: 0}
	h := newTestSamplingHandler(mgr)
	h.executor = &mockSamplingExecutor{output: "sampled response"}

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "hello"}},
			},
			MaxTokens: 100,
		},
	}

	result, err := h.HandleCreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if got, want := string(result.Role), "assistant"; got != want {
		t.Fatalf("role = %q, want %q", got, want)
	}
	if got, want := result.Model, "acorn-default"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := result.StopReason, "endTurn"; got != want {
		t.Fatalf("stopReason = %q, want %q", got, want)
	}
	tc, ok := result.Content.(*mcp.TextContent)
	if !ok {
		t.Fatal("content is not TextContent")
	}
	if got, want := tc.Text, "sampled response"; got != want {
		t.Fatalf("content text = %q, want %q", got, want)
	}

	// Verify depth was incremented and then decremented
	if got, want := atomic.LoadInt32(&mgr.samplingDepth), int32(0); got != want {
		t.Fatalf("samplingDepth after call = %d, want %d", got, want)
	}
}

func TestHandleCreateMessageDepthCapExceeded(t *testing.T) {
	mgr := &Manager{samplingDepth: 2} // already at cap
	h := newTestSamplingHandler(mgr)
	h.executor = &mockSamplingExecutor{output: "should not be called"}

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "hello"}},
			},
			MaxTokens: 100,
		},
	}

	_, err := h.HandleCreateMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when depth cap exceeded")
	}
	if !strings.Contains(err.Error(), "sampling depth cap exceeded") {
		t.Fatalf("error = %q, want 'sampling depth cap exceeded'", err.Error())
	}

	// Verify executor was not called
	if got, want := h.executor.(*mockSamplingExecutor).callCount(), 0; got != want {
		t.Fatalf("executor call count = %d, want %d", got, want)
	}

	// Verify depth was not permanently incremented (should remain 2)
	if got, want := atomic.LoadInt32(&mgr.samplingDepth), int32(2); got != want {
		t.Fatalf("samplingDepth after rejected call = %d, want %d", got, want)
	}
}

func TestHandleCreateMessageSubRunIDPrefix(t *testing.T) {
	mgr := &Manager{samplingDepth: 0}
	h := newTestSamplingHandler(mgr)

	var capturedMessages []*schema.Message
	h.executor = &mockSamplingExecutor{
		output: "response",
	}

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "test"}},
			},
			MaxTokens: 100,
		},
	}

	result, err := h.HandleCreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	// Check events emitted with sub-run ID
	store := h.store.(*mockSamplingEventStore)
	events := store.getEvents()
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}

	// Find the started event and verify sub-run ID is present
	var startedFound bool
	for _, ev := range events {
		if ev.kind == string(stream.StreamKindSamplingStarted) {
			startedFound = true
			if strings.TrimSpace(ev.payload.RunID) == "" {
				t.Fatal("sub-run ID in sampling.started event is empty")
			}
		}
	}
	if !startedFound {
		t.Fatal("expected sampling.started event")
	}

	_ = capturedMessages
}

func TestHandleCreateMessageEmitsEvents(t *testing.T) {
	mgr := &Manager{samplingDepth: 0}
	h := newTestSamplingHandler(mgr)
	h.executor = &mockSamplingExecutor{output: "response"}

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "test"}},
			},
			MaxTokens: 100,
		},
	}

	_, err := h.HandleCreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}

	store := h.store.(*mockSamplingEventStore)
	events := store.getEvents()

	// Expect sampling.started then sampling.completed
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].kind, string(stream.StreamKindSamplingStarted); got != want {
		t.Fatalf("event[0].kind = %q, want %q", got, want)
	}
	if got, want := events[1].kind, string(stream.StreamKindSamplingCompleted); got != want {
		t.Fatalf("event[1].kind = %q, want %q", got, want)
	}
	// Depth should be 1 during started event (after increment)
	if got, want := events[0].payload.Depth, int32(1); got != want {
		t.Fatalf("started event depth = %d, want %d", got, want)
	}
	// Completed event should have model set
	if got, want := events[1].payload.Model, "acorn-default"; got != want {
		t.Fatalf("completed event model = %q, want %q", got, want)
	}
}

func TestHandleCreateMessageExecutorError(t *testing.T) {
	mgr := &Manager{samplingDepth: 0}
	h := newTestSamplingHandler(mgr)
	h.executor = &mockSamplingExecutor{err: errors.New("LLM failed")}

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "test"}},
			},
			MaxTokens: 100,
		},
	}

	_, err := h.HandleCreateMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when executor fails")
	}
	if !strings.Contains(err.Error(), "LLM failed") {
		t.Fatalf("error = %q, want 'LLM failed'", err.Error())
	}

	// Verify sampling.failed event was emitted
	store := h.store.(*mockSamplingEventStore)
	events := store.getEvents()

	// Expect sampling.started then sampling.failed
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].kind, string(stream.StreamKindSamplingStarted); got != want {
		t.Fatalf("event[0].kind = %q, want %q", got, want)
	}
	if got, want := events[1].kind, string(stream.StreamKindSamplingFailed); got != want {
		t.Fatalf("event[1].kind = %q, want %q", got, want)
	}

	// Depth should be decremented back to 0
	if got, want := atomic.LoadInt32(&mgr.samplingDepth), int32(0); got != want {
		t.Fatalf("samplingDepth after error = %d, want %d", got, want)
	}
}

func TestHandleCreateMessageNoExecutor(t *testing.T) {
	mgr := &Manager{samplingDepth: 0}
	h := newTestSamplingHandler(mgr)
	// executor is nil

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "test"}},
			},
			MaxTokens: 100,
		},
	}

	_, err := h.HandleCreateMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when executor is nil")
	}
	if !strings.Contains(err.Error(), "sampling executor not configured") {
		t.Fatalf("error = %q, want 'sampling executor not configured'", err.Error())
	}

	// Depth should be decremented back to 0
	if got, want := atomic.LoadInt32(&mgr.samplingDepth), int32(0); got != want {
		t.Fatalf("samplingDepth after nil executor = %d, want %d", got, want)
	}
}

func TestHandleCreateMessageConcurrentDepthCap(t *testing.T) {
	mgr := &Manager{samplingDepth: 0}
	h := newTestSamplingHandler(mgr)

	enterCh := make(chan struct{}, 2)
	releaseCh := make(chan struct{})

	h.executor = &barrierMockExecutor{
		output:    "response",
		enterCh:   enterCh,
		releaseCh: releaseCh,
	}

	req := &mcp.CreateMessageRequest{
		Params: &mcp.CreateMessageParams{
			Messages: []*mcp.SamplingMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "test"}},
			},
			MaxTokens: 100,
		},
	}

	// Hold two in-flight requests inside ExecuteMessages so the third request
	// deterministically observes samplingDepth > samplingDepthCap.
	var inFlight [2]error
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			_, err := h.HandleCreateMessage(context.Background(), req)
			inFlight[idx] = err
		}()
	}

	for i := 0; i < 2; i++ {
		<-enterCh
	}

	_, rejectedErr := h.HandleCreateMessage(context.Background(), req)
	if rejectedErr == nil || !strings.Contains(rejectedErr.Error(), "sampling depth cap exceeded") {
		t.Fatalf("expected third request to be rejected due to depth cap, got %v", rejectedErr)
	}

	close(releaseCh)
	wg.Wait()

	for _, err := range inFlight {
		if err != nil {
			t.Fatalf("expected blocked requests to complete successfully, got %v", err)
		}
	}

	if got, want := atomic.LoadInt32(&mgr.samplingDepth), int32(0); got != want {
		t.Fatalf("samplingDepth after concurrent test = %d, want %d", got, want)
	}
}

// barrierMockExecutor blocks on a channel barrier to allow concurrent depth testing.
type barrierMockExecutor struct {
	output    string
	enterCh   chan<- struct{}
	releaseCh <-chan struct{}
}

func (b *barrierMockExecutor) ExecuteMessages(_ context.Context, _ []*schema.Message) (string, error) {
	if b.enterCh != nil {
		b.enterCh <- struct{}{}
	}
	if b.releaseCh != nil {
		<-b.releaseCh
	}
	return b.output, nil
}
