package tool

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	storerepo "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/store/storetest"
	"github.com/ycvk/acorn/internal/tooling"
)

// auditTestStore is the narrow store contract needed by tool-audit tests.
type auditTestStore interface {
	runtimeapi.EventAppender
	CreateRun(ctx context.Context, runID, input, checkpointID string) error
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
}

type fakeAuditTestStore struct {
	runs   map[string]events.RunRecord
	events map[string][]events.EventRecord
	next   int64
}

func newFakeAuditTestStore() *fakeAuditTestStore {
	return &fakeAuditTestStore{
		runs:   make(map[string]events.RunRecord),
		events: make(map[string][]events.EventRecord),
	}
}

func (s *fakeAuditTestStore) CreateRun(_ context.Context, runID, input, checkpointID string) error {
	now := time.Now().UTC()
	s.runs[runID] = events.RunRecord{
		RunID:        runID,
		Status:       events.RunStatusRunning,
		Input:        input,
		CheckpointID: checkpointID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return nil
}

func (s *fakeAuditTestStore) AppendEventContext(_ context.Context, runID, kind string, payload any) (events.EventRecord, error) {
	if _, ok := s.runs[runID]; !ok {
		return events.EventRecord{}, fmt.Errorf("run %q not found", runID)
	}
	s.next++
	record := events.EventRecord{
		Sequence:  s.next,
		RunID:     runID,
		Kind:      kind,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	s.events[runID] = append(s.events[runID], record)
	return record, nil
}

func (s *fakeAuditTestStore) LoadEvents(_ context.Context, runID string) ([]events.EventRecord, error) {
	return append([]events.EventRecord(nil), s.events[runID]...), nil
}

func TestAuditedToolDoesNotPersistToolCallEvents(t *testing.T) {
	store := openAuditTestStore(t)
	tool := mustInferTool(t, "echo", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	wrapped := mustWrapTool(t, store, "local", tool)

	invokable := wrapped.(einotool.InvokableTool)
	runCtx := runtimeapi.WithRunID(context.Background(), "run_success")
	if err := store.CreateRun(context.Background(), "run_success", "input", "run_success"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	result, err := invokable.InvokableRun(runCtx, `{"x":1}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if result != "ok" {
		t.Fatalf("unexpected tool result: %s", result)
	}

	evts, err := store.LoadEvents(context.Background(), "run_success")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}

func TestAuditedToolForwardsProgressWithoutPersistingChunks(t *testing.T) {
	store := openAuditTestStore(t)
	wrapped := mustWrapTool(t, store, "local", progressAuditTool{})

	invokable := wrapped.(tooling.ProgressTool)
	runCtx := withToolAuditCallID(runtimeapi.WithRunID(context.Background(), "run_progress"), "call_progress")
	if err := store.CreateRun(context.Background(), "run_progress", "input", "run_progress"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	var chunks []string
	result, err := invokable.InvokableRunWithProgress(runCtx, `{}`, func(_ context.Context, event tooling.ToolProgressEvent) error {
		chunks = append(chunks, event.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if result != "done" {
		t.Fatalf("unexpected tool result: %s", result)
	}
	if len(chunks) != 1 || chunks[0] != "chunk" {
		t.Fatalf("progress chunks = %#v, want [chunk]", chunks)
	}

	evts, err := store.LoadEvents(context.Background(), "run_progress")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}

func TestAuditedToolPropagatesInterruptWithoutPersistingEvents(t *testing.T) {
	store := openAuditTestStore(t)
	tool := mustInferTool(t, "pause_tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "", einotool.Interrupt(ctx, "need approval")
	})
	wrapped := mustWrapTool(t, store, "local", tool)

	invokable := wrapped.(einotool.InvokableTool)
	runCtx := runtimeapi.WithRunID(context.Background(), "run_interrupt")
	if err := store.CreateRun(context.Background(), "run_interrupt", "input", "run_interrupt"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err := invokable.InvokableRun(runCtx, `{}`)
	if err == nil {
		t.Fatal("expected interrupt error")
	}

	evts, err := store.LoadEvents(context.Background(), "run_interrupt")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}

func TestBuildAuditedToolsIncludesLocalAndMCPTools(t *testing.T) {
	store := openAuditTestStore(t)
	localTool := mustInferTool(t, "local_tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	mcpTool := mustInferTool(t, "mcp_tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})

	items, err := BuildAuditedTools(context.Background(), store, []tooling.ToolSpec{
		mustAuditSpec(t, "local", localTool),
		mustAuditSpec(t, "fixture", mcpTool),
	}, nil, nil, "")
	if err != nil {
		t.Fatalf("build audited tools: %v", err)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("expected %d tools, got %d", want, got)
	}
}

func TestBuildAuditedToolsHonorsExcludeList(t *testing.T) {
	store := openAuditTestStore(t)
	excludedTool := mustInferTool(t, "excluded_tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "delegated", nil
	})
	localTool := mustInferTool(t, "local_tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})

	items, err := BuildAuditedTools(context.Background(), store, []tooling.ToolSpec{
		mustAuditSpec(t, "local", excludedTool),
		mustAuditSpec(t, "local", localTool),
	}, []string{"excluded_tool"}, nil, "")
	if err != nil {
		t.Fatalf("build audited tools: %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}
	info, err := items[0].Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	if info.Name != "local_tool" {
		t.Fatalf("tool name = %q, want local_tool", info.Name)
	}
}

func TestBuildAuditedToolsHonorsAllowList(t *testing.T) {
	store := openAuditTestStore(t)
	firstTool := mustInferTool(t, "read_file", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	secondTool := mustInferTool(t, "run_command", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})

	items, err := BuildAuditedTools(context.Background(), store, []tooling.ToolSpec{
		mustAuditSpec(t, "local", firstTool),
		mustAuditSpec(t, "local", secondTool),
	}, nil, []string{"read_file"}, "")
	if err != nil {
		t.Fatalf("build audited tools: %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}
	info, err := items[0].Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	if info.Name != "read_file" {
		t.Fatalf("tool name = %q, want read_file", info.Name)
	}
}

func TestWrapToolForAuditFailsWhenValidatorCannotBeBuilt(t *testing.T) {
	store := openAuditTestStore(t)
	_, err := wrapToolForAudit(context.Background(), store, mustAuditSpec(t, "local", brokenAuditTool{}))
	if err == nil {
		t.Fatal("expected validator construction error")
	}
	if !strings.Contains(err.Error(), `create tool argument validator for "broken_tool"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func openAuditTestStore(t *testing.T) auditTestStore {
	t.Helper()
	return newFakeAuditTestStore()
}

func mustInferTool[T any, R any](t *testing.T, name string, fn func(context.Context, T) (R, error)) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, fn)
	if err != nil {
		t.Fatalf("infer tool: %v", err)
	}
	return tool
}

func mustWrapTool(t *testing.T, store runtimeapi.EventAppender, provider string, tool einotool.BaseTool) einotool.BaseTool {
	t.Helper()
	wrapped, err := wrapToolForAudit(context.Background(), store, mustAuditSpec(t, provider, tool))
	if err != nil {
		t.Fatalf("wrap tool: %v", err)
	}
	return wrapped
}

func mustAuditSpec(t *testing.T, provider string, tool einotool.BaseTool) tooling.ToolSpec {
	t.Helper()
	spec, err := RuntimeToolSpec(context.Background(), defaultAuditToolConfig(), provider, tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, tool)
	if err != nil {
		t.Fatalf("runtimeToolSpec: %v", err)
	}
	return spec
}

func defaultAuditToolConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Workspace:  config.WorkspaceToolConfig{RootDir: "."},
			Mutation:   config.MutationToolConfig{RootDir: "."},
			RunCommand: config.RunCommandToolConfig{WorkDir: "."},
		},
	}
}

func TestRuntimeToolSpecLoadToolsIsNeverParallel(t *testing.T) {
	spec, err := RuntimeToolSpec(
		context.Background(),
		defaultAuditToolConfig(),
		"runtime",
		tooling.ToolKindNative,
		[]tooling.ToolProfile{tooling.ToolProfileRun},
		mustInferTool(t, "load_tools", func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		}),
	)
	if err != nil {
		t.Fatalf("runtimeToolSpec: %v", err)
	}
	if spec.Execution.ParallelPolicy != tooling.ParallelPolicySerial {
		t.Fatalf("load_tools parallel policy = %q, want %q", spec.Execution.ParallelPolicy, tooling.ParallelPolicySerial)
	}
	if spec.Execution.PathArg != "" {
		t.Fatalf("load_tools path arg = %q, want empty", spec.Execution.PathArg)
	}
}

type brokenAuditTool struct{}

func (brokenAuditTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "broken_tool",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"bad": {
				Type: schema.DataType("not-a-real-json-schema-type"),
			},
		}),
	}, nil
}

func (brokenAuditTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "", nil
}

type progressAuditTool struct{}

func (progressAuditTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "progress_tool"}, nil
}

func (progressAuditTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "done", nil
}

func (progressAuditTool) InvokableRunWithProgress(ctx context.Context, _ string, emit tooling.ToolProgressEmitter, _ ...einotool.Option) (string, error) {
	if emit != nil {
		if err := emit(ctx, tooling.ToolProgressEvent{Delta: "chunk"}); err != nil {
			return "", err
		}
	}
	return "done", nil
}

func TestValidationBlocksInvalidArguments(t *testing.T) {
	store := openAuditTestStore(t)
	tool := mustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path" jsonschema:"required"`
		Content string `json:"content" jsonschema:"required"`
	}) (string, error) {
		return "ok", nil
	})
	wrapped := mustWrapTool(t, store, "local", tool)
	invokable := wrapped.(einotool.InvokableTool)

	runCtx := runtimeapi.WithRunID(context.Background(), "run_validation_block")
	if err := store.CreateRun(context.Background(), "run_validation_block", "input", "run_validation_block"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	result, err := invokable.InvokableRun(runCtx, `{}`)
	if err == nil {
		t.Fatal("expected validation failure error")
	}
	if !strings.Contains(result, "validation_failed") {
		t.Fatalf("expected validation error JSON, got %q", result)
	}

	evts, err := store.LoadEvents(context.Background(), "run_validation_block")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}

func TestValidationFailureThroughSafeParallelNodeIsModelVisibleFailedToolResult(t *testing.T) {
	store := openAuditTestStore(t)
	tool := mustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path" jsonschema:"required"`
		Content string `json:"content" jsonschema:"required"`
	}) (string, error) {
		return "ok", nil
	})
	wrapped := mustWrapTool(t, store, "local", tool)
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{wrapped}, fixedReadOnlyClassifier("write_file"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	const runID = "run_validation_node"
	const callID = "call_validation"
	if err := store.CreateRun(context.Background(), runID, "input", runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	ledger := storetest.NewMemoryToolResultLedger()
	ctx := safeParallelLifecycleContextFromWithLedger(
		t,
		runtimeapi.WithTurnIndex(runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_validation_node"), runID), 1),
		node,
		ledger,
	)

	results, err := invokeViaStreaming(node, ctx, makeAssistantMessage(
		makeToolCall(callID, "write_file", `{}`),
	))
	if err != nil {
		t.Fatalf("validation failure should produce failed ToolMessage, not run error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	msg := results[0]
	if msg.ToolCallID != callID || msg.Content == "" {
		t.Fatalf("expected validation failure tool message, got %+v", msg)
	}
	if !strings.Contains(msg.Content, "validation_failed") {
		t.Fatalf("tool message should expose validation failure JSON, got %q", msg.Content)
	}
	if failed, _ := msg.Extra["tool_error"].(bool); !failed {
		t.Fatalf("validation failure should be marked as tool_error: %+v", msg.Extra)
	}

	record, err := ledger.Load(context.Background(), storerepo.BuildToolResultRef(runID, callID))
	if err != nil {
		t.Fatalf("load ledger record: %v", err)
	}
	if record.Status != storerepo.ToolResultStatusFailed {
		t.Fatalf("ledger status = %q, want %q", record.Status, storerepo.ToolResultStatusFailed)
	}
	if record.FullText != msg.Content {
		t.Fatalf("ledger full text = %q, want tool message content %q", record.FullText, msg.Content)
	}

	evts, err := store.LoadEvents(context.Background(), runID)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}

func TestValidationAllowsValidArguments(t *testing.T) {
	store := openAuditTestStore(t)
	tool := mustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path" jsonschema:"required"`
		Content string `json:"content" jsonschema:"required"`
	}) (string, error) {
		return "ok", nil
	})
	wrapped := mustWrapTool(t, store, "local", tool)
	invokable := wrapped.(einotool.InvokableTool)

	runCtx := runtimeapi.WithRunID(context.Background(), "run_validation_allow")
	if err := store.CreateRun(context.Background(), "run_validation_allow", "input", "run_validation_allow"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	result, err := invokable.InvokableRun(runCtx, `{"path":"/tmp/test","content":"hello"}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if result != "ok" {
		t.Fatalf("unexpected tool result: %s", result)
	}

	evts, err := store.LoadEvents(context.Background(), "run_validation_allow")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}

func TestValidationFailuresDoNotCreateRepairState(t *testing.T) {
	store := openAuditTestStore(t)
	tool := mustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path" jsonschema:"required"`
		Content string `json:"content" jsonschema:"required"`
	}) (string, error) {
		return "ok", nil
	})
	wrapped := mustWrapTool(t, store, "local", tool)
	invokable := wrapped.(einotool.InvokableTool)

	runCtx := runtimeapi.WithRunID(context.Background(), "run_validation_no_repair_state")
	if err := store.CreateRun(context.Background(), "run_validation_no_repair_state", "input", "run_validation_no_repair_state"); err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Invalid calls remain ordinary failed tool calls; there is no repair counter.
	result, err := invokable.InvokableRun(runCtx, `{}`)
	if err == nil || !strings.Contains(result, "validation_failed") {
		t.Fatalf("call 1: result=%q err=%v, want validation failure", result, err)
	}
	result, err = invokable.InvokableRun(runCtx, `{}`)
	if err == nil || !strings.Contains(result, "validation_failed") {
		t.Fatalf("call 2: result=%q err=%v, want validation failure", result, err)
	}

	result, err = invokable.InvokableRun(runCtx, `{"path":"/tmp/test","content":"hello"}`)
	if err != nil || result != "ok" {
		t.Fatalf("valid call: result=%q err=%v, want ok", result, err)
	}

	evts, err := store.LoadEvents(context.Background(), "run_validation_no_repair_state")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(evts) != 0 {
		t.Fatalf("expected no persisted tool call audit events, got %#v", evts)
	}
}
