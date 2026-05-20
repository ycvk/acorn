package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/toolresult"
	"github.com/ycvk/acorn/internal/workspace"
)

type trackingTool struct {
	name      string
	result    string
	delay     time.Duration
	calls     atomic.Int64
	mu        sync.Mutex
	callTimes []time.Time
}

func (t *trackingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *trackingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	t.mu.Lock()
	t.callTimes = append(t.callTimes, time.Now())
	t.mu.Unlock()
	t.calls.Add(1)
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	if t.result == "" {
		return defaultTrackingToolResult(t.name, argumentsInJSON), nil
	}
	return t.result, nil
}

func (t *trackingTool) lastCallCount() int64 {
	return t.calls.Load()
}

func (t *trackingTool) callTimestamps() []time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]time.Time, len(t.callTimes))
	copy(out, t.callTimes)
	return out
}

type fixedClassifier struct {
	rules map[string]tooling.ParallelPolicy
}

func (c *fixedClassifier) ExecutionPolicy(toolName string, args map[string]any) (tooling.ToolExecutionPolicy, error) {
	if s, ok := c.rules[toolName]; ok {
		policy := tooling.ToolExecutionPolicy{ParallelPolicy: s}
		if s != tooling.ParallelPolicyNeverParallel {
			policy.PathArg = "path"
		}
		if toolName == "apply_unified_patch" {
			policy.PathArg = "paths"
		}
		if s == tooling.ParallelPolicyWriteScoped && toolName != "apply_unified_patch" {
			policy.PathArg = "path"
		}
		return policy, nil
	}
	return tooling.ToolExecutionPolicy{}, fmt.Errorf("missing execution policy for %s", toolName)
}

func defaultTrackingToolResult(toolName string, argumentsJSON string) string {
	switch strings.TrimSpace(toolName) {
	case "create_file", "replace_span":
		path := firstArgumentPath(argumentsJSON)
		if path == "" {
			path = "test.txt"
		}
		return fmt.Sprintf(`{"path":%q,"message":"ok","checkpoint_id":"checkpoint_%s","checkpoint_paths":[%q],"verified_bytes":0,"verified_content":"","verification_truncated":false}`, path, toolName, path)
	case "apply_unified_patch":
		paths := []string{firstArgumentPath(argumentsJSON)}
		if trimmed := strings.TrimSpace(paths[0]); trimmed == "" {
			paths = []string{"test.txt"}
		}
		return fmt.Sprintf(`{"paths":[%q],"message":"ok","checkpoint_id":"checkpoint_%s","checkpoint_paths":[%q],"verified_diff_stat":"1 file changed"}`, paths[0], toolName, paths[0])
	case "rollback_workspace_checkpoint":
		return `{"checkpoint_id":"checkpoint_test","rollback_id":"rollback_test","status":"succeeded","restored_paths":["test.txt"],"conflict_paths":[],"error":""}`
	default:
		return fmt.Sprintf("%s_result", toolName)
	}
}

func firstArgumentPath(argumentsJSON string) string {
	var payload struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return ""
	}
	if trimmed := strings.TrimSpace(payload.Path); trimmed != "" {
		return trimmed
	}
	if len(payload.Paths) > 0 {
		return strings.TrimSpace(payload.Paths[0])
	}
	return ""
}

func fixedReadOnlyClassifier(names ...string) *fixedClassifier {
	rules := make(map[string]tooling.ParallelPolicy, len(names))
	for _, name := range names {
		rules[name] = tooling.ParallelPolicyReadOnly
	}
	return &fixedClassifier{rules: rules}
}

var kernel = struct {
	ToolSafetyReadOnly      tooling.ParallelPolicy
	ToolSafetyWriteScoped   tooling.ParallelPolicy
	ToolSafetyNeverParallel tooling.ParallelPolicy
}{
	ToolSafetyReadOnly:      tooling.ParallelPolicyReadOnly,
	ToolSafetyWriteScoped:   tooling.ParallelPolicyWriteScoped,
	ToolSafetyNeverParallel: tooling.ParallelPolicyNeverParallel,
}

func makeAssistantMessage(calls ...schema.ToolCall) *schema.Message {
	return &schema.Message{
		Role:      schema.Assistant,
		ToolCalls: calls,
	}
}

func makeToolCall(id, name, args string) schema.ToolCall {
	return schema.ToolCall{
		ID: id,
		Function: schema.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func safeParallelLifecycleContext(t *testing.T, node *SafeParallelToolsNode) context.Context {
	t.Helper()
	ctx := withTurnIndex(withRunID(withSessionID(context.Background(), "sess_safe_parallel"), "run_safe_parallel"), 1)
	return safeParallelLifecycleContextFrom(t, ctx, node)
}

func invokeViaStreaming(node *SafeParallelToolsNode, ctx context.Context, input *schema.Message) ([]*schema.Message, error) {
	if input.Role != schema.Assistant {
		return nil, fmt.Errorf("safe parallel tools node: expected Assistant role, got %s", input.Role)
	}
	executor := node.NewStreamingExecutor(ctx)
	for _, call := range input.ToolCalls {
		executor.Submit(call)
	}
	return executor.GetRemainingResults(ctx)
}

func streamViaStreaming(node *SafeParallelToolsNode, ctx context.Context, input *schema.Message) ([]*schema.Message, error) {
	return invokeViaStreaming(node, ctx, input)
}

func safeParallelLifecycleContextFrom(t *testing.T, ctx context.Context, node *SafeParallelToolsNode) context.Context {
	return safeParallelLifecycleContextFromWithLedger(t, ctx, node, newMemoryToolResultLedger())
}

func safeParallelLifecycleContextFromWithLedger(t *testing.T, ctx context.Context, node *SafeParallelToolsNode, ledger toolresult.Ledger) context.Context {
	t.Helper()
	sessionID := sessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "sess_safe_parallel"
		ctx = withSessionID(ctx, sessionID)
	}
	runID := getRunID(ctx)
	if strings.TrimSpace(runID) == "" {
		runID = "run_safe_parallel"
		ctx = withRunID(ctx, runID)
	}
	state := &contextplane.ToolLifecycleState{
		RunID:         runID,
		SessionID:     sessionID,
		LoadedTools:   make(map[string]contextplane.LoadedToolRecord, len(node.tools)),
		DeferredTools: make(map[string]contextplane.DeferredToolRecord),
		MaxAgeTurns:   2,
		MaxResultRefs: 32,
	}
	infos := make([]*schema.ToolInfo, 0, len(node.tools))
	for name, entry := range node.tools {
		state.LoadedTools[name] = contextplane.LoadedToolRecord{Name: name, LoadSource: "test"}
		info, err := entry.Tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool Info: %v", err)
		}
		if info != nil {
			infos = append(infos, info)
		}
	}
	if ledger == nil {
		ledger = newMemoryToolResultLedger()
	}
	return contextplane.WithToolLifecycleContext(ctx, contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{ToolResultLedger: ledger}), state, nil, infos)
}

func safeParallelLifecycleContextWithDeferred(t *testing.T, node *SafeParallelToolsNode, deferredToolNames ...string) context.Context {
	t.Helper()
	ctx := safeParallelLifecycleContext(t, node)
	lifecycleCtx := contextplane.ToolLifecycleContextFromContext(ctx)
	for _, name := range deferredToolNames {
		delete(lifecycleCtx.State.LoadedTools, name)
		lifecycleCtx.State.DeferredTools[name] = contextplane.DeferredToolRecord{Name: name, Reason: "test_deferred"}
	}
	return ctx
}

func TestSafeParallel_AllReadOnlyExecutesInParallel(t *testing.T) {
	readA := &trackingTool{name: "read_a"}
	readB := &trackingTool{name: "read_b"}
	readC := &trackingTool{name: "read_c"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_a": kernel.ToolSafetyReadOnly,
			"read_b": kernel.ToolSafetyReadOnly,
			"read_c": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA, readB, readC},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_a", `{}`),
		makeToolCall("call_2", "read_b", `{}`),
		makeToolCall("call_3", "read_c", `{}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if readA.lastCallCount() != 1 || readB.lastCallCount() != 1 || readC.lastCallCount() != 1 {
		t.Fatalf("each tool should be called once: read_a=%d read_b=%d read_c=%d",
			readA.lastCallCount(), readB.lastCallCount(), readC.lastCallCount())
	}

	if results[0].ToolCallID != "call_1" || results[1].ToolCallID != "call_2" || results[2].ToolCallID != "call_3" {
		t.Fatalf("results not in original order: got %s, %s, %s",
			results[0].ToolCallID, results[1].ToolCallID, results[2].ToolCallID)
	}
}

func TestSafeParallel_NeverParallelExecutesSequentially(t *testing.T) {
	shellA := &trackingTool{name: "run_command"}
	shellB := &trackingTool{name: "run_command_2"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"run_command":   kernel.ToolSafetyNeverParallel,
			"run_command_2": kernel.ToolSafetyNeverParallel,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{shellA, shellB},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "run_command", `{}`),
		makeToolCall("call_2", "run_command_2", `{}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	timesA := shellA.callTimestamps()
	timesB := shellB.callTimestamps()
	if len(timesA) == 0 || len(timesB) == 0 {
		t.Fatal("missing call timestamps")
	}
	if timesB[0].Before(timesA[0]) {
		t.Fatal("run_command_2 should run after run_command")
	}
}

func TestSafeParallel_WriteScopedNoConflictExecutesInParallel(t *testing.T) {
	writeTool := &trackingTool{name: "create_file"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"create_file": kernel.ToolSafetyWriteScoped,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{writeTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "create_file", `{"path":"a.go","content":"hello"}`),
		makeToolCall("call_2", "create_file", `{"path":"b.go","content":"world"}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if writeTool.lastCallCount() != 2 {
		t.Fatalf("create_file should be called twice: got %d", writeTool.lastCallCount())
	}
}

func TestSafeParallelMutationToolAttachesSideEffects(t *testing.T) {
	writeTool := &trackingTool{
		name:   "create_file",
		result: `{"path":"a.go","bytes":5,"message":"ok","checkpoint_id":"workspace_checkpoint_1","checkpoint_paths":["a.go"],"verified_bytes":5,"verified_content":"hello","verification_truncated":false}`,
	}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"create_file": kernel.ToolSafetyWriteScoped,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{writeTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	ledger := newMemoryToolResultLedger()
	ctx := safeParallelLifecycleContextFromWithLedger(t, withTurnIndex(withRunID(withSessionID(context.Background(), "sess_safe_parallel"), "run_safe_parallel"), 1), node, ledger)
	results, err := invokeViaStreaming(node, ctx, makeAssistantMessage(
		makeToolCall("call_1", "create_file", `{"path":"a.go","content":"hello"}`),
	))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	rawSideEffects, ok := results[0].Extra["tool_side_effects"]
	if !ok {
		t.Fatal("tool_side_effects missing from tool message extra")
	}
	sideEffects, ok := rawSideEffects.([]toolresult.SideEffectRef)
	if !ok {
		t.Fatalf("tool_side_effects type = %T, want []toolresult.SideEffectRef", rawSideEffects)
	}
	if len(sideEffects) != 1 || sideEffects[0].Kind != workspace.MutationCheckpointEffect || sideEffects[0].Ref != "workspace_checkpoint_1" || sideEffects[0].Path != "a.go" {
		t.Fatalf("side effects = %+v", sideEffects)
	}
	record, err := ledger.Load(context.Background(), "tool_result:run_safe_parallel:call_1")
	if err != nil {
		t.Fatalf("load ledger record: %v", err)
	}
	if len(record.SideEffects) != 1 || record.SideEffects[0].Kind != workspace.MutationCheckpointEffect || record.SideEffects[0].Ref != "workspace_checkpoint_1" || record.SideEffects[0].Path != "a.go" {
		t.Fatalf("ledger side effects = %+v", record.SideEffects)
	}
}

func TestSafeParallel_WriteScopedPathConflictExecutesSequentially(t *testing.T) {
	writeTool := &trackingTool{name: "create_file"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"create_file": kernel.ToolSafetyWriteScoped,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{writeTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "create_file", `{"path":"a.go","content":"hello"}`),
		makeToolCall("call_2", "create_file", `{"path":"a.go","content":"world"}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	timestamps := writeTool.callTimestamps()
	if len(timestamps) != 2 {
		t.Fatalf("expected 2 call timestamps, got %d", len(timestamps))
	}
	if timestamps[1].Before(timestamps[0]) {
		t.Fatal("conflicting writes should run sequentially")
	}
}

func TestSafeParallel_WriteReadConflictExecutesSequentially(t *testing.T) {
	writeTool := &trackingTool{name: "create_file"}
	readTool := &trackingTool{name: "read_file", delay: 100 * time.Millisecond}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"create_file": kernel.ToolSafetyWriteScoped,
			"read_file":   kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{writeTool, readTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_file", `{"path":"a.go"}`),
		makeToolCall("call_2", "create_file", `{"path":"a.go","content":"hello"}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	readTimes := readTool.callTimestamps()
	writeTimes := writeTool.callTimestamps()
	if len(readTimes) != 1 || len(writeTimes) != 1 {
		t.Fatalf("unexpected call counts: read=%d write=%d", len(readTimes), len(writeTimes))
	}
	if writeTimes[0].Before(readTimes[0].Add(80 * time.Millisecond)) {
		t.Fatal("conflicting read/write calls should not overlap")
	}
}

func TestSafeParallelStreamingExecutor_WriteReadConflictExecutesSequentially(t *testing.T) {
	readTool := &trackingTool{name: "read_file", delay: 100 * time.Millisecond}
	patchTool := &trackingTool{name: "apply_unified_patch"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_file":           kernel.ToolSafetyReadOnly,
			"apply_unified_patch": kernel.ToolSafetyWriteScoped,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readTool, patchTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	executor := node.NewStreamingExecutor(safeParallelLifecycleContext(t, node))
	executor.Submit(makeToolCall("call_1", "read_file", `{"path":"a.go"}`))
	executor.Submit(makeToolCall("call_2", "apply_unified_patch", `{"paths":["a.go"],"patch":"diff --git a/a.go b/a.go"}`))

	results, err := executor.GetRemainingResults(context.Background())
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	readTimes := readTool.callTimestamps()
	patchTimes := patchTool.callTimestamps()
	if len(readTimes) != 1 || len(patchTimes) != 1 {
		t.Fatalf("unexpected call counts: read=%d patch=%d", len(readTimes), len(patchTimes))
	}
	if patchTimes[0].Before(readTimes[0].Add(80 * time.Millisecond)) {
		t.Fatal("streaming executor should serialize read/write path conflicts")
	}
}

func TestSafeParallel_MixedBatch(t *testing.T) {
	readA := &trackingTool{name: "read_file"}
	readB := &trackingTool{name: "memory_search"}
	writeC := &trackingTool{name: "create_file"}
	shellD := &trackingTool{name: "run_command"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_file":     kernel.ToolSafetyReadOnly,
			"memory_search": kernel.ToolSafetyReadOnly,
			"create_file":   kernel.ToolSafetyWriteScoped,
			"run_command":   kernel.ToolSafetyNeverParallel,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA, readB, writeC, shellD},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_file", `{"path":"x.go"}`),
		makeToolCall("call_2", "memory_search", `{"query":"test"}`),
		makeToolCall("call_3", "create_file", `{"path":"y.go","content":"hi"}`),
		makeToolCall("call_4", "run_command", `{"command":"ls"}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	if results[0].ToolCallID != "call_1" ||
		results[1].ToolCallID != "call_2" ||
		results[2].ToolCallID != "call_3" ||
		results[3].ToolCallID != "call_4" {
		t.Fatalf("results not in original order: %s, %s, %s, %s",
			results[0].ToolCallID, results[1].ToolCallID,
			results[2].ToolCallID, results[3].ToolCallID)
	}

	if readA.lastCallCount() != 1 || readB.lastCallCount() != 1 ||
		writeC.lastCallCount() != 1 || shellD.lastCallCount() != 1 {
		t.Fatalf("each tool should be called once: read=%d search=%d write=%d shell=%d",
			readA.lastCallCount(), readB.lastCallCount(),
			writeC.lastCallCount(), shellD.lastCallCount())
	}
}

func TestSafeParallel_ResultsPreserveOriginalOrder(t *testing.T) {
	readA := &trackingTool{name: "read_a"}
	shellB := &trackingTool{name: "run_command"}
	readC := &trackingTool{name: "read_c"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_a":      kernel.ToolSafetyReadOnly,
			"run_command": kernel.ToolSafetyNeverParallel,
			"read_c":      kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA, shellB, readC},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_a", `{}`),
		makeToolCall("call_2", "run_command", `{}`),
		makeToolCall("call_3", "read_c", `{}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if results[0].ToolCallID != "call_1" ||
		results[1].ToolCallID != "call_2" ||
		results[2].ToolCallID != "call_3" {
		t.Fatalf("order not preserved: %s, %s, %s",
			results[0].ToolCallID, results[1].ToolCallID, results[2].ToolCallID)
	}
}

func TestSafeParallel_EmptyBatchReturnsError(t *testing.T) {
	classifier := &fixedClassifier{rules: map[string]tooling.ParallelPolicy{}}
	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage()
	_, err = invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err == nil {
		t.Fatal("expected error for empty tool calls")
	}
}

func TestSafeParallel_SingleToolWorks(t *testing.T) {
	readA := &trackingTool{name: "read_a"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_a": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_a", `{}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ToolCallID != "call_1" {
		t.Fatalf("wrong tool call ID: %s", results[0].ToolCallID)
	}
}

func TestSafeParallel_RejectsNilExecutionPolicyResolver(t *testing.T) {
	readTool := &trackingTool{name: "read_file"}

	_, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readTool},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "execution policy resolver is required") {
		t.Fatalf("expected resolver error, got %v", err)
	}
}

func TestSafeParallel_UnknownToolReturnsErrorMessage(t *testing.T) {
	readA := &trackingTool{name: "read_a"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_a": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "nonexistent_tool", `{}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("expected unknown tool to produce error ToolMessage, not propagate error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ToolCallID != "call_1" {
		t.Fatalf("wrong tool call ID: %s", results[0].ToolCallID)
	}
	if results[0].Content == "" {
		t.Fatal("expected non-empty error content in ToolMessage")
	}
	if !strings.Contains(results[0].Content, "not loaded or enabled") {
		t.Fatalf("expected lifecycle rejection content, got %q", results[0].Content)
	}
	if failed, _ := results[0].Extra["tool_error"].(bool); !failed {
		t.Fatalf("unknown tool result should be marked as tool_error: %+v", results[0].Extra)
	}
	if readA.lastCallCount() != 0 {
		t.Fatalf("known tool should not execute for unknown tool rejection, got %d calls", readA.lastCallCount())
	}
}

func TestSafeParallel_MissingLifecycleFailsBeforeInvokingTool(t *testing.T) {
	readA := &trackingTool{name: "read_a"}
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{readA}, fixedReadOnlyClassifier("read_a"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	_, err = invokeViaStreaming(node, context.Background(), makeAssistantMessage(makeToolCall("call_1", "read_a", `{}`)))
	if err == nil || !strings.Contains(err.Error(), "tool lifecycle context is not initialized") {
		t.Fatalf("expected missing lifecycle context error, got %v", err)
	}
	if readA.lastCallCount() != 0 {
		t.Fatalf("tool should not execute without lifecycle context, got %d calls", readA.lastCallCount())
	}
}

func TestSafeParallel_DeferredToolReturnsFailedToolMessage(t *testing.T) {
	readA := &trackingTool{name: "read_a"}
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{readA}, fixedReadOnlyClassifier("read_a"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	results, err := invokeViaStreaming(node,
		safeParallelLifecycleContextWithDeferred(t, node, "read_a"),
		makeAssistantMessage(makeToolCall("call_1", "read_a", `{}`)),
	)
	if err != nil {
		t.Fatalf("deferred tool should produce failed ToolMessage, not runtime error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "deferred") {
		t.Fatalf("expected deferred rejection content, got %q", results[0].Content)
	}
	if failed, _ := results[0].Extra["tool_error"].(bool); !failed {
		t.Fatalf("deferred tool result should be marked as tool_error: %+v", results[0].Extra)
	}
	if readA.lastCallCount() != 0 {
		t.Fatalf("deferred tool should not execute before load, got %d calls", readA.lastCallCount())
	}
}

func TestSafeParallel_NonAssistantRoleReturnsError(t *testing.T) {
	classifier := &fixedClassifier{rules: map[string]tooling.ParallelPolicy{}}
	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := &schema.Message{Role: schema.User, Content: "hello"}
	_, err = invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err == nil {
		t.Fatal("expected error for non-Assistant role")
	}
}

func TestSafeParallel_StreamReturnsResults(t *testing.T) {
	readA := &trackingTool{name: "read_a"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_a": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_a", `{}`),
	)

	results, err := streamViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 message, got %d", len(results))
	}
	if results[0].ToolCallID != "call_1" {
		t.Fatalf("unexpected stream result: %+v", results[0])
	}
}

func TestSafeParallel_ParallelExecutionIsConcurrent(t *testing.T) {
	var started atomic.Int32
	var concurrentMax atomic.Int32

	slowRead := &slowTrackingTool{
		name: "slow_read",
		onInvokeStart: func() {
			current := started.Add(1)
			for {
				old := concurrentMax.Load()
				if current > old {
					if concurrentMax.CompareAndSwap(old, current) {
						break
					}
				} else {
					break
				}
			}
		},
		onInvokeEnd: func() {
			time.Sleep(50 * time.Millisecond)
			started.Add(-1)
		},
	}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"slow_read": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{slowRead},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "slow_read", `{}`),
		makeToolCall("call_2", "slow_read", `{}`),
		makeToolCall("call_3", "slow_read", `{}`),
	)

	_, err = invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if concurrentMax.Load() < 2 {
		t.Fatalf("expected concurrent execution (max concurrent >= 2), got %d", concurrentMax.Load())
	}
}

type slowTrackingTool struct {
	name          string
	onInvokeStart func()
	onInvokeEnd   func()
}

func (t *slowTrackingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *slowTrackingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	if t.onInvokeStart != nil {
		t.onInvokeStart()
	}
	if t.onInvokeEnd != nil {
		t.onInvokeEnd()
	}
	return fmt.Sprintf("%s_result", t.name), nil
}

type failingTool struct {
	name   string
	err    error
	result string
}

func (t *failingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *failingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.result, t.err
}

func TestSafeParallel_ToolFailureProducesErrorToolMessage(t *testing.T) {
	failTool := &failingTool{
		name:   "failing_read",
		err:    fmt.Errorf("file not found: /missing.go"),
		result: "",
	}
	okTool := &trackingTool{name: "ok_read"}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"failing_read": kernel.ToolSafetyReadOnly,
			"ok_read":      kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{failTool, okTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "failing_read", `{"path":"/missing.go"}`),
		makeToolCall("call_2", "ok_read", `{"path":"/exists.go"}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("tool failure should produce error ToolMessage, not propagate error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].ToolCallID != "call_1" {
		t.Fatalf("first result should map to call_1, got %s", results[0].ToolCallID)
	}
	if results[0].Content == "" {
		t.Fatal("failing tool result should contain error message for LLM")
	}
	if failed, _ := results[0].Extra["tool_error"].(bool); !failed {
		t.Fatalf("failing tool result should be marked as tool_error: %+v", results[0].Extra)
	}

	if results[1].ToolCallID != "call_2" {
		t.Fatalf("second result should map to call_2, got %s", results[1].ToolCallID)
	}
	if results[1].Extra != nil {
		if failed, _ := results[1].Extra["tool_error"].(bool); failed {
			t.Fatalf("successful tool result should not be marked as tool_error: %+v", results[1].Extra)
		}
	}
	if okTool.lastCallCount() != 1 {
		t.Fatalf("ok_read should still be called once despite other tool failing, got %d", okTool.lastCallCount())
	}
}

func TestSafeParallel_ToolFailureWithPartialOutput(t *testing.T) {
	failTool := &failingTool{
		name:   "partial_read",
		err:    fmt.Errorf("truncated output"),
		result: "first 100 bytes of file...",
	}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"partial_read": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{failTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "partial_read", `{}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("tool failure with partial output should produce ToolMessage, not error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "first 100 bytes of file..." {
		t.Fatalf("expected partial output as ToolMessage content, got %q", results[0].Content)
	}
	if failed, _ := results[0].Extra["tool_error"].(bool); !failed {
		t.Fatalf("partial tool result should be marked as tool_error: %+v", results[0].Extra)
	}
}

func TestSafeParallel_EinoToolCallbacksOnSuccess(t *testing.T) {
	readTool := &trackingTool{name: "read_file", result: "file contents"}
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{readTool}, fixedReadOnlyClassifier("read_file"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	var starts atomic.Int64
	var ends atomic.Int64
	var startName string
	var endName string
	var startCallID string
	var endResponse string
	handler := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			if info == nil || info.Component != components.ComponentOfTool {
				return ctx
			}
			starts.Add(1)
			startName = info.Name
			toolInput := einotool.ConvCallbackInput(input)
			if toolInput != nil {
				if value, ok := toolInput.Extra["tool_call_id"].(string); ok {
					startCallID = value
				}
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			if info == nil || info.Component != components.ComponentOfTool {
				return ctx
			}
			ends.Add(1)
			endName = info.Name
			toolOutput := einotool.ConvCallbackOutput(output)
			if toolOutput != nil {
				endResponse = toolOutput.Response
			}
			return ctx
		}).
		Build()

	ctx := callbacks.InitCallbacks(context.Background(), &callbacks.RunInfo{Name: "test"}, handler)
	results, err := invokeViaStreaming(node,
		safeParallelLifecycleContextFrom(t, ctx, node),
		makeAssistantMessage(makeToolCall("call_1", "read_file", `{"path":"a.go"}`)),
	)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(results) != 1 || results[0].Content != "file contents" {
		t.Fatalf("unexpected tool result: %+v", results)
	}
	if starts.Load() != 1 || ends.Load() != 1 {
		t.Fatalf("expected one tool start/end callback, got starts=%d ends=%d", starts.Load(), ends.Load())
	}
	if startName != "read_file" || endName != "read_file" {
		t.Fatalf("unexpected callback names: start=%q end=%q", startName, endName)
	}
	if startCallID != "call_1" {
		t.Fatalf("expected tool_call_id in callback extra, got %q", startCallID)
	}
	if endResponse != "file contents" {
		t.Fatalf("expected callback output response, got %q", endResponse)
	}
}

func TestSafeParallel_EinoToolCallbacksOnOrdinaryToolError(t *testing.T) {
	failTool := &failingTool{
		name: "read_file",
		err:  errors.New("disk read failed"),
	}
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{failTool}, fixedReadOnlyClassifier("read_file"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	var errorsSeen atomic.Int64
	var callbackErr string
	handler := callbacks.NewHandlerBuilder().
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			if info == nil || info.Component != components.ComponentOfTool {
				return ctx
			}
			errorsSeen.Add(1)
			if err != nil {
				callbackErr = err.Error()
			}
			return ctx
		}).
		Build()

	ctx := callbacks.InitCallbacks(context.Background(), &callbacks.RunInfo{Name: "test"}, handler)
	results, err := invokeViaStreaming(node,
		safeParallelLifecycleContextFrom(t, ctx, node),
		makeAssistantMessage(makeToolCall("call_1", "read_file", `{"path":"missing.go"}`)),
	)
	if err != nil {
		t.Fatalf("ordinary tool error should produce failed ToolMessage, not graph error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if failed, _ := results[0].Extra["tool_error"].(bool); !failed {
		t.Fatalf("expected failed ToolMessage, got %+v", results[0].Extra)
	}
	if errorsSeen.Load() != 1 {
		t.Fatalf("expected one Eino tool error callback, got %d", errorsSeen.Load())
	}
	if callbackErr != "disk read failed" {
		t.Fatalf("unexpected callback error: %q", callbackErr)
	}
}

func TestSafeParallel_InvalidArgumentsProduceToolMessage(t *testing.T) {
	readTool := &trackingTool{name: "read_file"}
	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_file": kernel.ToolSafetyReadOnly,
		},
	}
	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), makeAssistantMessage(
		makeToolCall("call_1", "read_file", `{"path":`),
	))
	if err != nil {
		t.Fatalf("invalid arguments should produce ToolMessage, not propagated error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ToolCallID != "call_1" || results[0].Content == "" {
		t.Fatalf("expected invalid-argument tool message, got %+v", results[0])
	}
	if readTool.lastCallCount() != 0 {
		t.Fatalf("tool should not execute with invalid JSON arguments, got %d calls", readTool.lastCallCount())
	}
}

func TestSafeParallelStreamingExecutor_InvalidArgumentsProduceToolMessage(t *testing.T) {
	readTool := &trackingTool{name: "read_file"}
	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_file": kernel.ToolSafetyReadOnly,
		},
	}
	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	executor := node.NewStreamingExecutor(safeParallelLifecycleContext(t, node))
	executor.Submit(makeToolCall("call_1", "read_file", `{"path":`))

	results, err := executor.GetRemainingResults(context.Background())
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if readTool.lastCallCount() != 0 {
		t.Fatalf("tool should not execute with invalid JSON arguments, got %d calls", readTool.lastCallCount())
	}
	if len(results) != 1 || results[0].ToolCallID != "call_1" {
		t.Fatalf("unexpected streaming tool results: %+v", results)
	}
	if failed, _ := results[0].Extra["tool_error"].(bool); !failed {
		t.Fatalf("expected failed ToolMessage, got %+v", results[0].Extra)
	}
}

func TestSafeParallel_RespectsMaxParallel(t *testing.T) {
	var active atomic.Int32
	var concurrentMax atomic.Int32
	slowRead := &slowTrackingTool{
		name: "slow_read",
		onInvokeStart: func() {
			current := active.Add(1)
			for {
				old := concurrentMax.Load()
				if current <= old {
					break
				}
				if concurrentMax.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		},
		onInvokeEnd: func() {
			active.Add(-1)
		},
	}
	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"slow_read": kernel.ToolSafetyReadOnly,
		},
	}
	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{slowRead},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	node.scheduler.maxParallel = 1

	_, err = invokeViaStreaming(node, safeParallelLifecycleContext(t, node), makeAssistantMessage(
		makeToolCall("call_1", "slow_read", `{}`),
		makeToolCall("call_2", "slow_read", `{}`),
		makeToolCall("call_3", "slow_read", `{}`),
	))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if concurrentMax.Load() > 1 {
		t.Fatalf("maxParallel=1 should serialize calls, got max concurrency %d", concurrentMax.Load())
	}
}

func TestSafeParallel_LoadToolsSerializesLifecycleMutation(t *testing.T) {
	var active atomic.Int32
	var concurrentMax atomic.Int32
	loadTools := mustInferTool(t, "load_tools", func(ctx context.Context, input map[string]any) (string, error) {
		current := active.Add(1)
		for {
			old := concurrentMax.Load()
			if current <= old {
				break
			}
			if concurrentMax.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return `{"loaded_tool_names":["memory_search"]}`, nil
	})
	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"load_tools": kernel.ToolSafetyNeverParallel,
		},
	}
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{loadTools}, classifier)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	_, err = invokeViaStreaming(node, safeParallelLifecycleContext(t, node), makeAssistantMessage(
		makeToolCall("call_1", "load_tools", `{"tool_names":["memory_search"]}`),
		makeToolCall("call_2", "load_tools", `{"tool_names":["memory_read_file"]}`),
	))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if concurrentMax.Load() > 1 {
		t.Fatalf("load_tools should serialize lifecycle mutation, got max concurrency %d", concurrentMax.Load())
	}
}

func TestSafeParallel_InterruptErrorPropagates(t *testing.T) {
	sig := &adk.InterruptSignal{}
	interruptTool := &failingTool{
		name:   "run_command",
		err:    sig,
		result: "",
	}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"run_command": kernel.ToolSafetyNeverParallel,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{interruptTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "run_command", `{"command":"rm -rf /"}`),
	)

	_, err = invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err == nil {
		t.Fatal("interrupt error should propagate, not be converted to ToolMessage")
	}
	if !isInterruptError(err) {
		t.Fatalf("expected interrupt error, got: %v", err)
	}
}

func TestSafeParallel_NonInvokableToolRejected(t *testing.T) {
	notInv := &baseOnlyTool{}

	classifier := &fixedClassifier{rules: map[string]tooling.ParallelPolicy{}}
	_, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{notInv},
		classifier,
	)
	if err == nil {
		t.Fatal("expected error for tool not implementing InvokableTool")
	}
}

type baseOnlyTool struct{}

func (t *baseOnlyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "base_only"}, nil
}

func TestSafeParallel_InferToolIntegration(t *testing.T) {
	echoTool, err := utils.InferTool("echo", "echoes input", func(ctx context.Context, input struct {
		Msg string `json:"msg"`
	}) (string, error) {
		return input.Msg, nil
	})
	if err != nil {
		t.Fatalf("InferTool: %v", err)
	}

	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"echo": kernel.ToolSafetyReadOnly,
		},
	}

	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{echoTool},
		classifier,
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "echo", `{"msg":"hello"}`),
	)

	results, err := invokeViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "hello" {
		t.Fatalf("expected 'hello', got %q", results[0].Content)
	}
}

func TestSafeParallel_StreamEmitsResultBatches(t *testing.T) {
	readA := &trackingTool{name: "read_a", result: "A"}
	readB := &trackingTool{name: "read_b", result: "B"}
	node, err := NewSafeParallelToolsNode(context.Background(),
		[]einotool.BaseTool{readA, readB},
		fixedReadOnlyClassifier("read_a", "read_b"),
	)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	input := makeAssistantMessage(
		makeToolCall("call_1", "read_a", `{}`),
		makeToolCall("call_2", "read_b", `{}`),
	)
	results, err := streamViaStreaming(node, safeParallelLifecycleContext(t, node), input)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	seen := map[string]string{}
	for _, msg := range results {
		if msg != nil {
			seen[msg.ToolCallID] = msg.Content
		}
	}
	if seen["call_1"] != "A" || seen["call_2"] != "B" {
		t.Fatalf("seen = %#v, want call_1=A call_2=B", seen)
	}
}

func TestSafeParallel_EmitsLifecycleTurnIndexFromContext(t *testing.T) {
	readTool := &trackingTool{name: "read_file", result: "README"}
	classifier := &fixedClassifier{
		rules: map[string]tooling.ParallelPolicy{
			"read_file": kernel.ToolSafetyReadOnly,
		},
	}
	node, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{readTool}, classifier)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	state := &contextplane.ToolLifecycleState{
		LoadedTools: map[string]contextplane.LoadedToolRecord{
			"read_file": {Name: "read_file", LoadSource: "eager"},
		},
	}
	ctx := withTurnIndex(withRunID(withSessionID(context.Background(), "sess_turn"), "run_turn"), 7)
	ctx = contextplane.WithToolLifecycleContext(ctx, contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{ToolResultLedger: newMemoryToolResultLedger()}), state, nil, []*schema.ToolInfo{{Name: "read_file"}})

	results, err := invokeViaStreaming(node, ctx, makeAssistantMessage(makeToolCall("call_1", "read_file", `{"path":"README.md"}`)))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if len(state.RecentResults) != 1 {
		t.Fatalf("recent results = %+v, want 1 record", state.RecentResults)
	}
	if state.RecentResults[0].TurnIndex != 7 {
		t.Fatalf("result turn index = %d, want 7", state.RecentResults[0].TurnIndex)
	}
}
