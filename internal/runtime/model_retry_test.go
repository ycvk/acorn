package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// eventLogStore records appended events; the executor only appends events
// while collecting run state.
type eventLogStore struct {
	core.SessionStore
	mu     sync.Mutex
	events []loggedEvent
}

type loggedEvent struct {
	kind    string
	payload map[string]any
}

func (s *eventLogStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := payload.(map[string]any)
	s.events = append(s.events, loggedEvent{kind: kind, payload: data})
	return core.EventRecord{RunID: runID, Sequence: int64(len(s.events))}, nil
}

func indexOfKind(events []loggedEvent, kind string) int {
	for i, event := range events {
		if event.kind == kind {
			return i
		}
	}
	return -1
}

// streamAttempt is one scripted model call: frames, then err when set.
type streamAttempt struct {
	frames []string
	err    error
}

type flakyStreamModel struct {
	mu       sync.Mutex
	attempts []streamAttempt
	calls    int
}

func (m *flakyStreamModel) Generate(context.Context, []*schema.AgenticMessage, ...einomodel.Option) (*schema.AgenticMessage, error) {
	return nil, errors.New("flaky stream model only streams")
}

func (m *flakyStreamModel) Stream(context.Context, []*schema.AgenticMessage, ...einomodel.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.attempts) {
		return nil, fmt.Errorf("flaky stream model exhausted after %d attempts", len(m.attempts))
	}
	attempt := m.attempts[m.calls]
	m.calls++
	reader, writer := schema.Pipe[*schema.AgenticMessage](len(attempt.frames) + 1)
	for _, frame := range attempt.frames {
		writer.Send(assistantMessage(frame, nil), nil)
	}
	if attempt.err != nil {
		writer.Send(nil, attempt.err)
	}
	writer.Close()
	return reader, nil
}

func collectRetryRun(t *testing.T, model *flakyStreamModel) (RunState, *eventLogStore) {
	t.Helper()
	ctx := context.Background()
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:             "retry_test",
		Description:      "model retry test agent",
		Model:            model,
		ModelRetryConfig: modelRetryConfig(),
		MaxIterations:    3,
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true})
	store := &eventLogStore{}
	iter := runner.Run(ctx, []adk.AgenticMessage{schema.UserAgenticMessage("hi")})
	state, err := (&Executor{store: store}).collectRunState(ctx, "run_retry", iter, nil, &ActiveRunner{ChatModel: model, FailedCalls: newFailedToolCalls()})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return state, store
}

func TestModelStreamFailureIsRetried(t *testing.T) {
	model := &flakyStreamModel{attempts: []streamAttempt{
		{frames: []string{"partial "}, err: errors.New("connection reset")},
		{frames: []string{"final ", "answer"}},
	}}
	state, store := collectRetryRun(t, model)
	if state.failure != nil {
		t.Fatalf("run failed after a successful retry: %v", state.failure)
	}
	if state.lastOutput != "final answer" {
		t.Fatalf("output = %q, want only the retried attempt's text", state.lastOutput)
	}
	var messages []string
	deltas := 0
	for _, event := range store.events {
		switch event.kind {
		case "runtime.message":
			msg, _ := event.payload["message"].(map[string]any)
			messages = append(messages, fmt.Sprint(msg["content"]))
		case "assistant.delta":
			deltas++
		case "run.failed":
			t.Fatalf("retried stream failure was recorded as run failure: %v", event.payload)
		}
	}
	if len(messages) != 1 || messages[0] != "final answer" {
		t.Fatalf("assistant messages = %q, want the retried message only", messages)
	}
	// Deltas the client already received from the failed attempt stay; the
	// final message replaces the streamed bubble.
	if deltas != 3 {
		t.Fatalf("deltas = %d, want 1 from the failed attempt and 2 from the retry", deltas)
	}
}

func TestModelStreamFailsAfterRetriesExhausted(t *testing.T) {
	fail := streamAttempt{frames: []string{"x"}, err: errors.New("provider overloaded")}
	model := &flakyStreamModel{attempts: []streamAttempt{fail, fail, fail, fail}}
	state, store := collectRetryRun(t, model)
	if state.failure == nil {
		t.Fatal("run succeeded although every attempt failed")
	}
	if model.calls != 4 {
		t.Fatalf("model calls = %d, want the first attempt plus 3 retries", model.calls)
	}
	if indexOfKind(store.events, "runtime.message") != -1 {
		t.Fatalf("a failed attempt produced an assistant message: %+v", store.events)
	}
}
