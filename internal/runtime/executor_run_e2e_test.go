package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/stream"
)

// e2eFakeModel is a configurable BaseChatModel for end-to-end Executor tests.
// It returns a predetermined assistant message on Stream (the path used by
// direct_response AgentLoop) and Generate. If streamErr is non-nil, Stream
// returns it instead, simulating a provider error mid-run.
type e2eFakeModel struct {
	content   string
	streamErr error
}

func (m *e2eFakeModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	return schema.AssistantMessage(m.content, nil), nil
}

func (m *e2eFakeModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	return schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage(m.content, nil),
	}), nil
}

// collectSink collects StreamItems for assertion without a real SSE channel.
func collectSink(out *[]stream.StreamItem) stream.StreamSink {
	return func(item stream.StreamItem) error {
		*out = append(*out, item)
		return nil
	}
}

func newE2EExecutor(t *testing.T, fakeModel einomodel.BaseChatModel) (*Executor, *RunnerFactory, *storesqlite.Store) {
	t.Helper()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	factory.installRunChatModelBuilderForTest(func(_ context.Context, _ RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return fakeModel, nil
	})
	exec, err := NewExecutorWithRunRuntimeAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunRuntimeAndController: %v", err)
	}
	return exec, factory, store
}

// TestExecuteMessagesDirectResponseSucceedsEndToEnd exercises the full
// ExecuteMessages main path: run creation → run.started emission → runner build
// → ContextSession bootstrap → model stream → assistant message persistence →
// run.completed → run status succeeded. It uses a real SQLite store and a real
// RunnerFactory with a fake chat model so the entire wiring (tool catalog,
// context plane, orchestration plane, stream projection, finalization) is
// exercised end-to-end.
//
// Unlike the mode-routing tests in orchestration_mode_test.go (which use a
// fake plane to截断 after routing), this test lets the run proceed through the
// real direct_response AgentLoop and verifies the terminal state + persisted
// assistant message + conversation history — the path users actually hit.
func TestExecuteMessagesDirectResponseSucceedsEndToEnd(t *testing.T) {
	ctx := context.Background()
	exec, _, store := newE2EExecutor(t, &e2eFakeModel{content: "hello from fake model"})

	var collected []stream.StreamItem
	sink := collectSink(&collected)

	result, err := exec.ExecuteMessages(ctx, runtimeapi.ExecuteRequest{
		Input: "test prompt",
	}, sink)
	if err != nil {
		t.Fatalf("ExecuteMessages: %v", err)
	}

	if result.Status != "succeeded" {
		t.Fatalf("result status = %q, want succeeded", result.Status)
	}
	if result.Output != "hello from fake model" {
		t.Fatalf("result output = %q, want %q", result.Output, "hello from fake model")
	}
	if strings.TrimSpace(result.RunID) == "" {
		t.Fatal("result RunID is empty")
	}

	// Verify the event sequence persisted to the store: run.started must come
	// before run.completed, and both must be present.
	records, err := store.LoadEvents(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var (
		startedIdx   = -1
		completedIdx = -1
	)
	for i, r := range records {
		switch r.Kind {
		case "run.started":
			startedIdx = i
		case "run.completed":
			completedIdx = i
		}
	}
	if startedIdx < 0 {
		t.Fatalf("run.started event not found in %d events", len(records))
	}
	if completedIdx < 0 {
		t.Fatalf("run.completed event not found in %d events", len(records))
	}
	if completedIdx < startedIdx {
		t.Fatalf("run.completed (idx %d) before run.started (idx %d)", completedIdx, startedIdx)
	}

	// The assistant output must be persisted as a session message so resume and
	// conversation history can recover it.
	messages, err := store.ListSessionMessagesByRunID(ctx, result.RunID)
	if err != nil {
		t.Fatalf("ListSessionMessagesByRunID: %v", err)
	}
	var foundAssistant bool
	for _, msg := range messages {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "hello from fake model") {
			foundAssistant = true
			break
		}
	}
	if !foundAssistant {
		t.Fatalf("assistant message with fake model output not persisted; messages: %+v", messages)
	}

	// The run record itself must reflect the terminal status.
	run, err := store.LoadRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != "succeeded" {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}
}

// TestExecuteMessagesDirectResponseFailsOnModelStreamError verifies that when
// the provider Stream returns an error, the run is marked failed and the
// failure is persisted as a run.failed event rather than silently swallowed.
// This is the fail-loud contract: provider errors become durable run truth,
// not silent failures.
func TestExecuteMessagesDirectResponseFailsOnModelStreamError(t *testing.T) {
	ctx := context.Background()
	modelErr := errors.New("simulated provider stream failure")
	exec, _, store := newE2EExecutor(t, &e2eFakeModel{streamErr: modelErr})

	var collected []stream.StreamItem
	sink := collectSink(&collected)

	result, err := exec.ExecuteMessages(ctx, runtimeapi.ExecuteRequest{
		Input: "test prompt that will fail",
	}, sink)
	// ExecuteMessages returns the Result with failed status, not a Go error,
	// because the failure is persisted as run truth before the function returns.
	if err != nil {
		t.Fatalf("ExecuteMessages returned error instead of failed result: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "simulated provider stream failure") {
		t.Fatalf("result error = %q, want it to contain the stream error", result.Error)
	}

	records, err := store.LoadEvents(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var foundFailed bool
	for _, r := range records {
		if r.Kind == "run.failed" {
			foundFailed = true
			if payload, ok := r.Payload.(map[string]any); ok {
				if msg, _ := payload["error"].(string); !strings.Contains(msg, "simulated provider stream failure") {
					t.Fatalf("run.failed payload error = %q, want stream error", msg)
				}
			}
			break
		}
	}
	if !foundFailed {
		t.Fatalf("run.failed event not found in %d events", len(records))
	}

	run, err := store.LoadRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
}
