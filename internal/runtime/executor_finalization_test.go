package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/stream"
)

func TestFinishCollectedRunSuccessPersistsTerminalEvent(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, ctx, store, "session-terminal", "hello")

	_, err := exec.finishCollectedRun(ctx, runID, "hello", RunState{lastOutput: "world"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	raw, err := store.LoadEvents(ctx, runID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1].Kind != "run.completed" {
		t.Fatalf("last event = %#v, want run.completed", raw)
	}

	messages, err := store.ListSessionMessagesByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("ListSessionMessagesByRunID: %v", err)
	}
	if got, want := len(messages), 2; got != want {
		t.Fatalf("session messages = %d, want %d", got, want)
	}
	if messages[1].Role != "assistant" || messages[1].Content != "world" {
		t.Fatalf("assistant message = (%q, %q), want (assistant, world)", messages[1].Role, messages[1].Content)
	}

	hit, err := store.GetConversationHistorySegmentByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("GetConversationHistorySegmentByRunID: %v", err)
	}
	if hit == nil || !strings.Contains(hit.Content, "world") {
		t.Fatalf("conversation segment content = %+v, want assistant output", hit)
	}
	assertMemoryHistoryContains(t, cfg, "session-terminal", runID, "succeeded hello world")
}

func TestFinishCollectedRunArchiveFailureMarksRunFailed(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	exec.archiveRunFunc = func(context.Context, string, events.RunStatus) error {
		return errors.New("archive writer unavailable")
	}
	runID := createFinalizationRun(t, ctx, store, "", "hello")

	_, err := exec.finishCollectedRun(ctx, runID, "hello", RunState{lastOutput: "world"}, nil, nil)
	if err == nil {
		t.Fatal("finishCollectedRun returned nil error, want archive finalization error")
	}
	if !strings.Contains(err.Error(), "archive writer unavailable") {
		t.Fatalf("error = %q, want archive error", err.Error())
	}

	run, err := store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusFailed {
		t.Fatalf("run status = %q, want %q", run.Status, events.RunStatusFailed)
	}
	if !strings.Contains(run.Error, "run finalization failed") {
		t.Fatalf("run error = %q, want finalization failure", run.Error)
	}

	raw, err := store.LoadEvents(ctx, runID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1].Kind != "run.failed" {
		t.Fatalf("last event = %#v, want run.failed", raw)
	}
}

func TestFinishCollectedRunAppendsMemoryHistoryWithTouchedFiles(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, ctx, store, "session-history", "update memory runtime")
	appendSuccessfulToolEvent(t, ctx, store, runID, "apply_unified_patch", `{"path":"internal/runtime/executor_terminal.go"}`)

	if _, err := exec.finishCollectedRun(ctx, runID, "update memory runtime", RunState{lastOutput: "history appended"}, nil, nil); err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	assertMemoryHistoryContains(t, cfg, "session-history", runID, "files changed: internal/runtime/executor_terminal.go")
}

func TestFinishCollectedRunSuccessPersistsAfterContextCancellation(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, context.Background(), store, "session-cancelled", "hello")

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.finishCollectedRun(cancelledCtx, runID, "hello", RunState{lastOutput: "world"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun with cancelled context: %v", err)
	}
	if result.Status != events.RunStatusSucceeded {
		t.Fatalf("result status = %q, want %q", result.Status, events.RunStatusSucceeded)
	}
	run, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusSucceeded {
		t.Fatalf("run status = %q, want %q", run.Status, events.RunStatusSucceeded)
	}
	if run.Output != "world" {
		t.Fatalf("run output = %q, want world", run.Output)
	}

	assertMemoryHistoryContains(t, cfg, "session-cancelled", runID, "succeeded hello world")
}

func newFinalizationTestExecutor(t *testing.T, store RunnerFactoryStore, cfg *config.Config) *Executor {
	runRuntime := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	exec := &Executor{
		store:             store,
		runRuntime:        runRuntime,
		controller:        NewRunController(),
		sessionSummarySvc: runRuntime.SessionSummarySvc(),
		newChatModel: func(context.Context) (einomodel.BaseChatModel, error) {
			return nil, errors.New("unexpected model creation")
		},
	}
	exec.archiveRunFunc = exec.archiveRun
	return exec
}

func createFinalizationRun(t *testing.T, ctx context.Context, store *storesqlite.Store, sessionID, input string) string {
	t.Helper()
	runID := "run_" + strings.ReplaceAll(sessionID, "-", "_")
	if sessionID == "" {
		runID = "run_standalone"
		if err := store.CreateBoundRun(ctx, runID, "", 0, input, runID); err != nil {
			t.Fatalf("CreateBoundRun: %v", err)
		}
		if _, err := stream.AppendStreamItem(ctx, store, nil, stream.StreamItem{RunID: runID, Kind: stream.StreamKindRunStarted, Payload: map[string]any{"input": input}}); err != nil {
			t.Fatalf("append run_started: %v", err)
		}
		return runID
	}
	if _, err := store.CreateSession(ctx, sessionID, "test"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turnIndex, _, err := store.PrepareChatTurn(ctx, sessionID, input, "test", 12)
	if err != nil {
		t.Fatalf("PrepareChatTurn: %v", err)
	}
	if err := store.CreateBoundRun(ctx, runID, sessionID, turnIndex, input, runID); err != nil {
		t.Fatalf("CreateBoundRun: %v", err)
	}
	if _, err := stream.AppendStreamItem(ctx, store, nil, stream.StreamItem{RunID: runID, Kind: stream.StreamKindRunStarted, Payload: map[string]any{"input": input}}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	return runID
}

func appendSuccessfulToolEvent(t *testing.T, ctx context.Context, store *storesqlite.Store, runID, name, argumentsJSON string) {
	t.Helper()
	if _, err := stream.AppendStreamItem(ctx, store, nil, stream.StreamItem{
		RunID: runID,
		Kind:  stream.StreamKindToolCallSucceeded,
		Payload: map[string]any{"tool_call": &stream.StreamToolCall{
			Name:          name,
			ArgumentsJSON: argumentsJSON,
			Output:        "ok",
		}},
	}); err != nil {
		t.Fatalf("append tool_call_succeeded: %v", err)
	}
}

func assertMemoryHistoryContains(t *testing.T, cfg *config.Config, sessionID string, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg.Runtime.StorageDir, "memory", "history", sessionID+".md"))
	if err != nil {
		t.Fatalf("read memory history: %v", err)
	}
	body := string(data)
	for _, part := range parts {
		if !strings.Contains(body, part) {
			t.Fatalf("history missing %q:\n%s", part, body)
		}
	}
}
