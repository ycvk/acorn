package dispatch

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// --- Test helpers ---

type stubResolver struct {
	policies map[string]core.ToolExecutionPolicy
}

func (r *stubResolver) ExecutionPolicy(toolName string, args map[string]any) (core.ToolExecutionPolicy, error) {
	if p, ok := r.policies[toolName]; ok {
		return p, nil
	}
	return core.ToolExecutionPolicy{ParallelPolicy: core.ParallelPolicySerial}, nil
}

func readOnlyPolicy() core.ToolExecutionPolicy {
	return core.ToolExecutionPolicy{ParallelPolicy: core.ParallelPolicyReadOnly}
}

func serialPolicy(pathArg string) core.ToolExecutionPolicy {
	return core.ToolExecutionPolicy{ParallelPolicy: core.ParallelPolicySerial, PathArg: pathArg}
}

func makeTool(t *testing.T, name string, result string) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, func(ctx context.Context, args map[string]any) (string, error) {
		return result, nil
	})
	if err != nil {
		t.Fatalf("infer tool %q: %v", name, err)
	}
	return tool
}

func makeFailingTool(t *testing.T, name string, toolErr error) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, func(ctx context.Context, args map[string]any) (string, error) {
		return "", toolErr
	})
	if err != nil {
		t.Fatalf("infer failing tool %q: %v", name, err)
	}
	return tool
}

func makeNode(t *testing.T, ctx context.Context, tools []einotool.BaseTool, resolver core.ExecutionPolicyResolver) *SafeParallelToolsNode {
	t.Helper()
	node, err := NewSafeParallelToolsNode(ctx, tools, resolver)
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	return node
}

func toolCall(id, name, args string) schema.ToolCall {
	return schema.ToolCall{
		ID: id,
		Function: schema.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

// --- invokeSingle tests (via Submit + GetRemainingResults) ---

func TestInvokeSingleNormalExecution(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "read_file", "file content here")},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"read_file": readOnlyPolicy(),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "read_file", `{"path":"foo.txt"}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "file content here" {
		t.Fatalf("content = %q, want %q", results[0].Content, "file content here")
	}
	if results[0].ToolName != "read_file" {
		t.Fatalf("tool name = %q, want %q", results[0].ToolName, "read_file")
	}
	if results[0].Extra["tool_result_ref"] == nil {
		t.Fatal("expected tool_result_ref in Extra")
	}
	if results[0].Extra["tool_arguments_json"] == nil {
		t.Fatal("expected tool_arguments_json in Extra")
	}
}

func TestInvokeSingleInvalidArgs(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "read_file", "unused")},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"read_file": readOnlyPolicy(),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "read_file", `{not valid json`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Extra["tool_error"] != true {
		t.Fatal("expected tool_error=true in Extra")
	}
	if !strings.Contains(results[0].Content, "Invalid arguments") {
		t.Fatalf("content should mention invalid args, got: %q", results[0].Content)
	}
}

func TestInvokeSingleToolNotFound(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	node, err := NewSafeParallelToolsNode(ctx, nil, &stubResolver{})
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "nonexistent_tool", `{}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Extra["tool_error"] != true {
		t.Fatal("expected tool_error=true in Extra")
	}
	if !strings.Contains(results[0].Content, "not found") {
		t.Fatalf("content should mention not found, got: %q", results[0].Content)
	}
}

func TestInvokeSingleToolExecutionError(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeFailingTool(t, "write_file", errors.New("permission denied"))},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"write_file": serialPolicy("path"),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "write_file", `{"path":"foo.txt"}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Extra["tool_error"] != true {
		t.Fatal("expected tool_error=true in Extra")
	}
	if !strings.Contains(results[0].Content, "permission denied") {
		t.Fatalf("content should contain error, got: %q", results[0].Content)
	}
}

// --- Parallel execution tests ---

func TestReadOnlyToolsExecuteInParallel(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	var maxConcurrent int32
	var current atomic.Int32

	makeConcurrentReadOnlyTool := func(name string) einotool.BaseTool {
		tool, _ := toolutils.InferTool(name, name, func(ctx context.Context, args map[string]any) (string, error) {
			cur := current.Add(1)
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			current.Add(-1)
			return name + " done", nil
		})
		return tool
	}

	node := makeNode(t, ctx,
		[]einotool.BaseTool{
			makeConcurrentReadOnlyTool("read_a"),
			makeConcurrentReadOnlyTool("read_b"),
			makeConcurrentReadOnlyTool("read_c"),
		},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"read_a": readOnlyPolicy(),
			"read_b": readOnlyPolicy(),
			"read_c": readOnlyPolicy(),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "read_a", `{}`))
	executor.Submit(toolCall("call-2", "read_b", `{}`))
	executor.Submit(toolCall("call-3", "read_c", `{}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if atomic.LoadInt32(&maxConcurrent) < 2 {
		t.Fatalf("expected at least 2 concurrent executions, max was %d", atomic.LoadInt32(&maxConcurrent))
	}
}
func TestSerialToolsWithOverlappingPathsAreSerialized(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	var maxConcurrent int32
	var current atomic.Int32

	makeSerialTool := func(name string) einotool.BaseTool {
		tool, _ := toolutils.InferTool(name, name, func(ctx context.Context, args map[string]any) (string, error) {
			cur := current.Add(1)
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			current.Add(-1)
			return "ok", nil
		})
		return tool
	}

	// Use plain tool names that don't trigger side-effect parsing
	node := makeNode(t, ctx,
		[]einotool.BaseTool{
			makeSerialTool("serial_a"),
			makeSerialTool("serial_b"),
		},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"serial_a": serialPolicy("path"),
			"serial_b": serialPolicy("path"),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "serial_a", `{"path":"foo.txt"}`))
	executor.Submit(toolCall("call-2", "serial_b", `{"path":"foo.txt"}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if atomic.LoadInt32(&maxConcurrent) > 1 {
		t.Fatalf("expected max 1 concurrent for overlapping paths, got %d", atomic.LoadInt32(&maxConcurrent))
	}
}

func TestSerialToolsWithDifferentPathsCanParallelize(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	var maxConcurrent int32
	var current atomic.Int32

	makeSerialTool := func(name string) einotool.BaseTool {
		tool, _ := toolutils.InferTool(name, name, func(ctx context.Context, args map[string]any) (string, error) {
			cur := current.Add(1)
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			current.Add(-1)
			return "ok", nil
		})
		return tool
	}

	// Register two tools with the same name but different paths
	node := makeNode(t, ctx,
		[]einotool.BaseTool{
			makeSerialTool("serial_c"),
		},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"serial_c": serialPolicy("path"),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "serial_c", `{"path":"a.txt"}`))
	executor.Submit(toolCall("call-2", "serial_c", `{"path":"b.txt"}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if atomic.LoadInt32(&maxConcurrent) < 2 {
		t.Fatalf("expected 2 concurrent for non-overlapping paths, max was %d", atomic.LoadInt32(&maxConcurrent))
	}
}

func TestGetRemainingResultsEmptySubmission(t *testing.T) {
	ctx := context.Background()
	node, err := NewSafeParallelToolsNode(ctx, nil, &stubResolver{})
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}

	executor := node.NewStreamingExecutor(ctx)
	_, err = executor.GetRemainingResults(ctx)
	if err == nil {
		t.Fatal("expected error for empty submission, got nil")
	}
	if !strings.Contains(err.Error(), "no tool calls") {
		t.Fatalf("expected 'no tool calls' error, got: %v", err)
	}
}

// --- Side effects tests ---

func TestSideEffectsCreateFile(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	result := `{"checkpoint_id":"ckpt-1","checkpoint_paths":["/repo/foo.txt"]}`
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "create_file", result)},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"create_file": serialPolicy("path"),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "create_file", `{"path":"/repo/foo.txt"}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	sideEffects, ok := results[0].Extra["tool_side_effects"].([]SideEffectRef)
	if !ok {
		t.Fatalf("expected tool_side_effects in Extra, got: %#v", results[0].Extra["tool_side_effects"])
	}
	if len(sideEffects) != 1 {
		t.Fatalf("expected 1 side effect, got %d", len(sideEffects))
	}
	if sideEffects[0].Ref != "ckpt-1" {
		t.Fatalf("ref = %q, want %q", sideEffects[0].Ref, "ckpt-1")
	}
	if sideEffects[0].Path != "/repo/foo.txt" {
		t.Fatalf("path = %q, want %q", sideEffects[0].Path, "/repo/foo.txt")
	}
}

func TestSideEffectsAskOperator(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	result := `{"action_id":"action-abc"}`
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "ask_operator", result)},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"ask_operator": serialPolicy(""),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "ask_operator", `{}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	sideEffects, ok := results[0].Extra["tool_side_effects"].([]SideEffectRef)
	if !ok {
		t.Fatalf("expected tool_side_effects, got: %#v", results[0].Extra["tool_side_effects"])
	}
	if len(sideEffects) != 1 {
		t.Fatalf("expected 1 side effect, got %d", len(sideEffects))
	}
	if sideEffects[0].Kind != SideEffectKindOperatorAction {
		t.Fatalf("kind = %q, want %q", sideEffects[0].Kind, SideEffectKindOperatorAction)
	}
	if sideEffects[0].Ref != "action-abc" {
		t.Fatalf("ref = %q, want %q", sideEffects[0].Ref, "action-abc")
	}
}

func TestSideEffectsArtifactWrite(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	result := `{"artifact_id":"art-123"}`
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "artifact_write", result)},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"artifact_write": readOnlyPolicy(),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "artifact_write", `{}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	sideEffects, ok := results[0].Extra["tool_side_effects"].([]SideEffectRef)
	if !ok {
		t.Fatalf("expected tool_side_effects, got: %#v", results[0].Extra["tool_side_effects"])
	}
	if len(sideEffects) != 1 {
		t.Fatalf("expected 1 side effect, got %d", len(sideEffects))
	}
	if sideEffects[0].Kind != SideEffectKindArtifact {
		t.Fatalf("kind = %q, want %q", sideEffects[0].Kind, SideEffectKindArtifact)
	}
	if sideEffects[0].Ref != "art-123" {
		t.Fatalf("ref = %q, want %q", sideEffects[0].Ref, "art-123")
	}
}

func TestSideEffectsNoSideEffectsForUnknownTool(t *testing.T) {
	ctx := core.WithRunID(context.Background(), "run-1")
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "plain_tool", "just a result")},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"plain_tool": readOnlyPolicy(),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "plain_tool", `{}`))

	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		t.Fatalf("GetRemainingResults: %v", err)
	}
	if results[0].Extra["tool_side_effects"] != nil {
		t.Fatalf("expected no side effects for unknown tool, got: %#v", results[0].Extra["tool_side_effects"])
	}
}

// --- ToolAuditCallID context tests ---

func TestToolAuditCallIDRoundTrip(t *testing.T) {
	ctx := WithToolAuditCallID(context.Background(), "call-abc")
	if got := ToolAuditCallID(ctx); got != "call-abc" {
		t.Fatalf("ToolAuditCallID = %q, want %q", got, "call-abc")
	}
}

func TestToolAuditCallIDEmptyContext(t *testing.T) {
	if got := ToolAuditCallID(context.Background()); got != "" {
		t.Fatalf("ToolAuditCallID = %q, want empty", got)
	}
}

// --- pathsOverlap tests ---

func TestPathsOverlap(t *testing.T) {
	cases := []struct {
		name   string
		left   []string
		right  []string
		expect bool
	}{
		{"both empty", nil, nil, false},
		{"one empty", []string{"a"}, nil, false},
		{"same path", []string{"a.txt"}, []string{"a.txt"}, true},
		{"different paths", []string{"a.txt"}, []string{"b.txt"}, false},
		{"partial overlap", []string{"a.txt", "b.txt"}, []string{"b.txt", "c.txt"}, true},
		{"whitespace trimmed", []string{"  a.txt  "}, []string{"a.txt"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathsOverlap(tc.left, tc.right); got != tc.expect {
				t.Fatalf("pathsOverlap(%v, %v) = %v, want %v", tc.left, tc.right, got, tc.expect)
			}
		})
	}
}

func TestBuildToolResultRef(t *testing.T) {
	want := "tool_result:run-1:call-1"
	if got := buildToolResultRef("run-1", "call-1"); got != want {
		t.Fatalf("buildToolResultRef = %q, want %q", got, want)
	}
	if got := buildToolResultRef("  run-1  ", "  call-1  "); got != want {
		t.Fatalf("buildToolResultRef with whitespace = %q, want %q", got, want)
	}
}

// --- side_effects JSON parsing edge cases ---

func TestSideEffectsMutationMissingCheckpointID(t *testing.T) {
	_, err := toolSideEffectsFromResult("create_file", `{"checkpoint_paths":["/repo/foo.txt"]}`)
	if err == nil || !strings.Contains(err.Error(), "missing checkpoint_id") {
		t.Fatalf("expected missing checkpoint_id error, got: %v", err)
	}
}

func TestSideEffectsMutationMissingPaths(t *testing.T) {
	_, err := toolSideEffectsFromResult("create_file", `{"checkpoint_id":"ckpt-1"}`)
	if err == nil || !strings.Contains(err.Error(), "missing checkpoint_paths") {
		t.Fatalf("expected missing checkpoint_paths error, got: %v", err)
	}
}

func TestSideEffectsArtifactWriteMissingID(t *testing.T) {
	_, err := toolSideEffectsFromResult("artifact_write", `{"not_artifact_id":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "missing artifact_id") {
		t.Fatalf("expected missing artifact_id error, got: %v", err)
	}
}

func TestSideEffectsInvalidJSON(t *testing.T) {
	_, err := toolSideEffectsFromResult("create_file", `{not json`)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestSideEffectsRollbackNotSucceeded(t *testing.T) {
	effects, err := toolSideEffectsFromResult("rollback_workspace_checkpoint", `{"status":"failed"}`)
	if err != nil || effects != nil {
		t.Fatalf("expected nil effects for non-succeeded rollback, got: effects=%#v err=%v", effects, err)
	}
}

func TestSideEffectsRollbackSucceeded(t *testing.T) {
	result := `{"rollback_id":"rb-1","status":"succeeded","restored_paths":["/repo/a.txt","/repo/b.txt"]}`
	effects, err := toolSideEffectsFromResult("rollback_workspace_checkpoint", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	for _, e := range effects {
		if e.Ref != "rb-1" {
			t.Fatalf("ref = %q, want %q", e.Ref, "rb-1")
		}
	}
}

func TestSideEffectsGitSummaryEmptyDiffArtifactID(t *testing.T) {
	effects, err := toolSideEffectsFromResult("git_summary", `{"diff_artifact_id":""}`)
	if err != nil || effects != nil {
		t.Fatalf("expected nil effects for empty diff_artifact_id, got: effects=%#v err=%v", effects, err)
	}
}

func TestSideEffectsRunVerificationMissingIDs(t *testing.T) {
	_, err := toolSideEffectsFromResult("run_verification", `{"stdout_artifact_id":"a"}`)
	if err == nil || !strings.Contains(err.Error(), "missing stdout_artifact_id or stderr_artifact_id") {
		t.Fatalf("expected missing IDs error, got: %v", err)
	}
}

// --- executionPathsFromArgs tests ---

func TestExecutionPathsFromArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     map[string]any
		pathArg  string
		required bool
		want     []string
		wantErr  bool
	}{
		{"string path", map[string]any{"path": "foo.txt"}, "path", true, []string{"foo.txt"}, false},
		{"array of strings", map[string]any{"paths": []any{"a.txt", "b.txt"}}, "paths", true, []string{"a.txt", "b.txt"}, false},
		{"missing required", map[string]any{}, "path", true, nil, true},
		{"missing optional", map[string]any{}, "path", false, nil, false},
		{"empty pathArg", map[string]any{"path": "foo"}, "", false, nil, false},
		{"nil args required", nil, "path", true, nil, true},
		{"nil args optional", nil, "path", false, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := executionPathsFromArgs(tc.args, tc.pathArg, tc.required)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- Discard test ---

func TestDiscardDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(core.WithRunID(context.Background(), "run-1"), 2*time.Second)
	defer cancel()
	node := makeNode(t, ctx,
		[]einotool.BaseTool{makeTool(t, "read_file", "content")},
		&stubResolver{policies: map[string]core.ToolExecutionPolicy{
			"read_file": readOnlyPolicy(),
		}})

	executor := node.NewStreamingExecutor(ctx)
	executor.Submit(toolCall("call-1", "read_file", `{}`))
	executor.Discard()

	// Discard sets discarded=true and cancels siblingCtx.
	// GetRemainingResults may return empty or results depending on timing.
	// The invariant is: Discard doesn't panic and prevents new execution.
	_, _ = executor.GetRemainingResults(ctx)
}
