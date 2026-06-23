package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/store"
)

// fakeExecutorStore implements ExecutorStore for testing consume/finishCollectedRun.
type fakeExecutorStore struct {
	mu                sync.Mutex
	appendedEvents    []fakeAppendedEvent
	finishedRuns      []fakeFinishedRun
	interruptedRuns   []string
	updatedOutputs    []string
	syncedRuns        []string
	createSessionErr  error
	createBoundRunErr error
}

type fakeAppendedEvent struct {
	runID   string
	kind    string
	payload any
	seq     int64
}

type fakeFinishedRun struct {
	runID   string
	status  domain.RunStatus
	output  string
	errText string
}

func (s *fakeExecutorStore) AppendEventContext(_ context.Context, runID, kind string, payload any) (domain.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := int64(len(s.appendedEvents) + 1)
	s.appendedEvents = append(s.appendedEvents, fakeAppendedEvent{runID: runID, kind: kind, payload: payload, seq: seq})
	return domain.EventRecord{RunID: runID, Kind: kind, Sequence: seq, CreatedAt: time.Now()}, nil
}

func (s *fakeExecutorStore) CreateFreshSessionTurn(_ context.Context, _, _, _ string) (int, error) {
	if s.createSessionErr != nil {
		return 0, s.createSessionErr
	}
	return 0, nil
}

func (s *fakeExecutorStore) CreateBoundRunWithParams(_ context.Context, _ store.RunCreateParams) error {
	return s.createBoundRunErr
}

func (s *fakeExecutorStore) LoadRun(_ context.Context, runID string) (*domain.RunRecord, error) {
	return &domain.RunRecord{RunID: runID, SessionID: "session_test"}, nil
}

func (s *fakeExecutorStore) FinishRunContext(_ context.Context, runID string, status domain.RunStatus, output, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishedRuns = append(s.finishedRuns, fakeFinishedRun{runID: runID, status: status, output: output, errText: errText})
	return nil
}

func (s *fakeExecutorStore) MarkInterruptedContext(_ context.Context, runID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interruptedRuns = append(s.interruptedRuns, runID)
	return nil
}

func (s *fakeExecutorStore) UpdateRunOutputContext(_ context.Context, runID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updatedOutputs = append(s.updatedOutputs, runID)
	return nil
}

func (s *fakeExecutorStore) LoadEvents(_ context.Context, _ string) ([]domain.EventRecord, error) {
	return nil, nil
}

func (s *fakeExecutorStore) LoadEventsAfter(_ context.Context, _ string, _ int64) ([]domain.EventRecord, error) {
	return nil, nil
}

func (s *fakeExecutorStore) SyncAssistantMessageForRun(_ context.Context, _ string) error {
	return nil
}

func (s *fakeExecutorStore) SyncAssistantMessageForRunStatus(_ context.Context, runID string, _ domain.RunStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncedRuns = append(s.syncedRuns, runID)
	return nil
}

// fakeChatModel is a minimal chat model satisfying einomodel.BaseChatModel for consume.
type fakeChatModel struct{}

func (m *fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage("done", nil), nil
}

func (m *fakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("done", nil)}), nil
}

func (m *fakeChatModel) BindTools(_ []*schema.ToolInfo, _ ...einomodel.Option) error { return nil }

// newTestRunnerFactory builds a minimal RunnerFactory for executor tests.
// Only MemoryModule and Config are exercised by consume/finishCollectedRun.
func newTestRunnerFactory(memSvc memory.Service, chatFunc func(context.Context) (einomodel.BaseChatModel, error)) *RunnerFactory {
	return &RunnerFactory{
		deps: RuntimeDeps{
			Config:       &config.Config{},
			MemoryModule: memSvc,
		},
		registry: NewRegistry(),
		modelBuilder: &ModelBuilder{
			cfg: &config.Config{},
			runChatModelBuilder: func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
				if chatFunc != nil {
					return chatFunc(context.Background())
				}
				return &fakeChatModel{}, nil
			},
		},
	}
}

func newTestExecutor(t *testing.T) (*Executor, *fakeExecutorStore) {
	t.Helper()
	memSvc, err := memory.NewLocalService(memory.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	st := &fakeExecutorStore{}
	return &Executor{
		store:        st,
		runRuntime:   newTestRunnerFactory(memSvc, nil),
		controller:   NewRunController(),
		newChatModel: func(context.Context) (einomodel.BaseChatModel, error) { return &fakeChatModel{}, nil },
	}, st
}

func TestConsumeHappyPathReturnsSucceeded(t *testing.T) {

	exec, st := newTestExecutor(t)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				Message: schema.AssistantMessage("hello world", nil),
			},
		},
	})
	gen.Close()

	result, err := exec.consume(context.Background(), "run_test", "input", iter, nil, nil, &fakeChatModel{})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if result.Status != domain.RunStatusSucceeded {
		t.Errorf("status = %q, want %q", result.Status, domain.RunStatusSucceeded)
	}
	if result.Output != "hello world" {
		t.Errorf("output = %q, want 'hello world'", result.Output)
	}
	if result.RunID != "run_test" {
		t.Errorf("run_id = %q, want 'run_test'", result.RunID)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.finishedRuns) != 1 {
		t.Fatalf("finishedRuns = %d, want 1", len(st.finishedRuns))
	}
	if st.finishedRuns[0].status != domain.RunStatusSucceeded {
		t.Errorf("finished status = %q, want succeeded", st.finishedRuns[0].status)
	}
	if len(st.appendedEvents) == 0 {
		t.Error("expected events to be appended to store")
	}
}

func TestConsumeRunFailureReturnsFailed(t *testing.T) {

	exec, st := newTestExecutor(t)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		Err: errors.New("model returned error"),
	})
	gen.Close()

	result, err := exec.consume(context.Background(), "run_fail", "input", iter, nil, nil, &fakeChatModel{})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if result.Status != domain.RunStatusFailed {
		t.Errorf("status = %q, want %q", result.Status, domain.RunStatusFailed)
	}
	if result.Error != "model returned error" {
		t.Errorf("error = %q, want 'model returned error'", result.Error)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.finishedRuns) != 1 {
		t.Fatalf("finishedRuns = %d, want 1", len(st.finishedRuns))
	}
	if st.finishedRuns[0].status != domain.RunStatusFailed {
		t.Errorf("finished status = %q, want failed", st.finishedRuns[0].status)
	}
}

func TestConsumeInterruptReturnsInterrupted(t *testing.T) {

	exec, st := newTestExecutor(t)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		Action: &adk.AgentAction{
			Interrupted: &adk.InterruptInfo{
				Data: map[string]any{"action_id": "pending_1"},
			},
		},
	})
	gen.Close()

	result, err := exec.consume(context.Background(), "run_interrupt", "input", iter, nil, nil, &fakeChatModel{})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if result.Status != domain.RunStatusInterrupted {
		t.Errorf("status = %q, want %q", result.Status, domain.RunStatusInterrupted)
	}
	if result.Interrupted == nil {
		t.Fatal("interrupted payload = nil, want non-nil")
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.interruptedRuns) != 1 {
		t.Fatalf("interruptedRuns = %d, want 1", len(st.interruptedRuns))
	}
	if st.interruptedRuns[0] != "run_interrupt" {
		t.Errorf("interrupted runID = %q, want 'run_interrupt'", st.interruptedRuns[0])
	}
}

func TestConsumeEmptyIteratorReturnsSucceededWithEmptyOutput(t *testing.T) {

	exec, _ := newTestExecutor(t)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()

	result, err := exec.consume(context.Background(), "run_empty", "input", iter, nil, nil, &fakeChatModel{})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if result.Status != domain.RunStatusSucceeded {
		t.Errorf("status = %q, want succeeded (empty iterator = success)", result.Status)
	}
	if result.Output != "" {
		t.Errorf("output = %q, want empty", result.Output)
	}
}

func TestConsumeStreamingEventThenMessageReplacesOutput(t *testing.T) {
	exec, _ := newTestExecutor(t)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	// First event: streaming output — IsStreaming=true causes GetMessage() to
	// consume the stream via concatMessageStream, producing an assistant_message
	// domain.StreamItem whose Content replaces lastOutput.
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: true,
				MessageStream: schema.StreamReaderFromArray([]*schema.Message{
					schema.AssistantMessage("partial stream output", nil),
				}),
			},
		},
	})
	// Second event: non-streaming message — replaces the streamed output.
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				Message: schema.AssistantMessage("final answer", nil),
			},
		},
	})
	gen.Close()

	result, err := exec.consume(context.Background(), "run_multi", "input", iter, nil, nil, &fakeChatModel{})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if result.Status != domain.RunStatusSucceeded {
		t.Errorf("status = %q, want succeeded", result.Status)
	}
	if result.Output != "final answer" {
		t.Errorf("output = %q, want 'final answer' (non-streaming message replaces streamed output)", result.Output)
	}
	// Verify both events produced StreamItems appended to the store.
	st := exec.store.(*fakeExecutorStore)
	st.mu.Lock()
	defer st.mu.Unlock()
	assistantMsgCount := 0
	for _, ev := range st.appendedEvents {
		if ev.kind == "agent.message" {
			assistantMsgCount++
		}
	}
	if assistantMsgCount != 2 {
		t.Errorf("assistant_message events = %d, want 2 (streaming + final)", assistantMsgCount)
	}
}

func TestConsumeSinkReceivesEvents(t *testing.T) {

	exec, _ := newTestExecutor(t)

	var sinkMu sync.Mutex
	var sinkItems []domain.StreamItem
	sink := func(item domain.StreamItem) error {
		sinkMu.Lock()
		sinkItems = append(sinkItems, item)
		sinkMu.Unlock()
		return nil
	}

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				Message: schema.AssistantMessage("streamed", nil),
			},
		},
	})
	gen.Close()

	_, err := exec.consume(context.Background(), "run_sink", "input", iter, nil, sink, &fakeChatModel{})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	sinkMu.Lock()
	defer sinkMu.Unlock()
	if len(sinkItems) == 0 {
		t.Fatal("sink received 0 items, want at least 1")
	}
}
