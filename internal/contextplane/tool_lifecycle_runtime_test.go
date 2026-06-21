package contextplane_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/tooling"
)

type lifecycleStubTool struct {
	name string
	desc string
}

func (t lifecycleStubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.desc}, nil
}

type lifecycleSnapshotStore struct{}

func (lifecycleSnapshotStore) SaveRunContextSnapshot(context.Context, model.RunContextSnapshot) error {
	return nil
}

func (lifecycleSnapshotStore) LoadRunContextSnapshot(context.Context, string) (*model.RunContextSnapshot, error) {
	return nil, nil
}

func testTokenCounter(t *testing.T) *contextplane.CompressionTokenCounter {
	t.Helper()
	counter, err := contextplane.NewCompressionTokenCounter()
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	return counter
}

func newLifecycleCatalogForTest(t *testing.T) *tooling.Catalog {
	t.Helper()
	catalog, err := tooling.NewCatalog(context.Background(), []tooling.ToolSpec{
		{
			ToolContract: lifecycleToolContract("read_file", "local", tooling.ToolKindNative, tooling.EagerLoadingPolicy()),
			Tool:         lifecycleStubTool{name: "read_file", desc: "Read a file"},
			Health:       tooling.ToolHealth{State: tooling.HealthStateHealthy},
		},
		{
			ToolContract: lifecycleToolContract("mcp.prompt.fetch", "mcp.prompt", tooling.ToolKindMCP, tooling.DeferredLoadingPolicy("deferred_mcp_catalog")),
			Tool:         lifecycleStubTool{name: "mcp.prompt.fetch", desc: "Fetch MCP prompt"},
			Health:       tooling.ToolHealth{State: tooling.HealthStateHealthy},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func lifecycleToolContract(name string, source string, kind tooling.ToolKind, loading tooling.ToolLoadingPolicy) tooling.ToolContract {
	return tooling.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      kind,
		Category:  tooling.ToolCategoryRead,
		Loading:   loading,
		Execution: tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicyReadOnly},
	}
}

func TestLoadedToolInfosFromContextExcludesDeferredUntilLoaded(t *testing.T) {
	plane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})
	catalog := newLifecycleCatalogForTest(t)
	result, err := plane.Assemble(context.Background(), contextplane.AssembleRequest{
		RunID:       "run-lifecycle-infos",
		SessionID:   "sess-lifecycle-infos",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	var toolInfos []*schema.ToolInfo
	for _, spec := range catalog.EnabledSpecs() {
		info, err := spec.Tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info %s: %v", spec.Name, err)
		}
		toolInfos = append(toolInfos, info)
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), result.LifecycleState, catalog, toolInfos)

	initial := contextplane.LoadedToolInfosFromContext(ctx, result.EagerToolNames)
	if len(initial) != 1 || initial[0].Name != "read_file" {
		t.Fatalf("initial loaded tool infos = %+v, want only read_file", initial)
	}
	if _, err := contextplane.DeferredLoad(ctx, contextplane.DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}}); err != nil {
		t.Fatalf("DeferredLoad: %v", err)
	}
	loaded := contextplane.LoadedToolInfosFromContext(ctx, result.EagerToolNames)
	if len(loaded) != 2 || loaded[0].Name != "mcp.prompt.fetch" || loaded[1].Name != "read_file" {
		t.Fatalf("loaded tool infos = %+v, want deferred tool after load plus read_file", loaded)
	}
}

func TestToolLifecycleOnToolCallRequiresLoadedTool(t *testing.T) {
	plane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})
	catalog := newLifecycleCatalogForTest(t)

	result, err := plane.Assemble(context.Background(), contextplane.AssembleRequest{
		RunID:       "run-lifecycle-call",
		SessionID:   "sess-lifecycle-call",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), result.LifecycleState, catalog, nil)

	if err := contextplane.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "read_file"}); err != nil {
		t.Fatalf("loaded tool should pass: %v", err)
	}
	err = contextplane.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "mcp.prompt.fetch"})
	var rejected *contextplane.ToolCallRejectedError
	if !errors.As(err, &rejected) || rejected.ToolName != "mcp.prompt.fetch" || !strings.Contains(rejected.Reason, "deferred") {
		t.Fatalf("expected deferred tool rejection, got %v", err)
	}
	err = contextplane.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "missing_tool"})
	rejected = nil
	if !errors.As(err, &rejected) || rejected.ToolName != "missing_tool" || !strings.Contains(rejected.Reason, "not loaded or enabled") {
		t.Fatalf("expected unknown tool rejection, got %v", err)
	}

	loadResult, err := contextplane.DeferredLoad(ctx, contextplane.DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}})
	if err != nil {
		t.Fatalf("DeferredLoad: %v", err)
	}
	if !slices.Equal(loadResult.LoadedToolNames, []string{"mcp.prompt.fetch"}) {
		t.Fatalf("loaded tool names = %+v", loadResult.LoadedToolNames)
	}
	if err := contextplane.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "mcp.prompt.fetch"}); err != nil {
		t.Fatalf("deferred-loaded tool should pass: %v", err)
	}
}

func TestDeferredLoadRejectsEmptyRequestAndInvalidLimit(t *testing.T) {
	if _, err := contextplane.DeferredLoad(context.Background(), contextplane.DeferredLoadRequest{}); err == nil {
		t.Fatal("expected empty request error")
	}
	if _, err := contextplane.DeferredLoad(context.Background(), contextplane.DeferredLoadRequest{
		Query: "git",
		Limit: 6,
	}); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestToolLifecycleEventsRequireToolName(t *testing.T) {
	if err := contextplane.OnToolCall(context.Background(), contextplane.ToolCallEvent{}); err == nil {
		t.Fatal("expected tool call validation error")
	}
	if err := contextplane.OnToolResult(context.Background(), contextplane.ToolResultEvent{}); err == nil {
		t.Fatal("expected tool result validation error")
	}
}

func TestToolLifecycleEventsRequireStateForNamedTools(t *testing.T) {
	if err := contextplane.OnToolCall(context.Background(), contextplane.ToolCallEvent{ToolName: "read_file"}); err == nil || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected lifecycle state runtime error, got %v", err)
	}
	if err := contextplane.OnToolResult(context.Background(), contextplane.ToolResultEvent{ToolName: "read_file", CallID: "call_1"}); err == nil || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected lifecycle state runtime error, got %v", err)
	}
}

func TestToolLifecycleOnToolResultPersistsRecentResults(t *testing.T) {
	state := &contextplane.ToolLifecycleState{
		RunID:         "run_ledger",
		SessionID:     "sess_ledger",
		LoadedTools:   map[string]contextplane.LoadedToolRecord{"read_file": {Name: "read_file"}},
		DeferredTools: map[string]contextplane.DeferredToolRecord{},
		MaxResultRefs: 32,
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), state, nil, nil)

	if err := contextplane.OnToolResult(ctx, contextplane.ToolResultEvent{
		RunID:        "run_ledger",
		SessionID:    "sess_ledger",
		TurnIndex:    3,
		CallID:       "call_1",
		ToolName:     "read_file",
		Arguments:    `{"path":"README.md"}`,
		Result:       "tool output body",
		ResultTokens: 4,
	}); err != nil {
		t.Fatalf("OnToolResult: %v", err)
	}

	if len(state.RecentResults) != 1 {
		t.Fatalf("recent result count = %d, want 1", len(state.RecentResults))
	}
	if got, want := state.RecentResults[0].ResultRef, "result:run_ledger:call_1"; got != want {
		t.Fatalf("recent result ref = %q, want %q", got, want)
	}
	if got, want := state.RecentResults[0].Summary, "tool output body"; got != want {
		t.Fatalf("recent result summary = %q, want %q", got, want)
	}
	if got, want := state.RecentResults[0].FullText, "tool output body"; got != want {
		t.Fatalf("recent result full text = %q, want %q", got, want)
	}
	if got, want := state.RecentResults[0].ToolName, "read_file"; got != want {
		t.Fatalf("recent result tool name = %q, want %q", got, want)
	}
	if got, want := state.RecentResults[0].TurnIndex, 3; got != want {
		t.Fatalf("recent result turn index = %d, want %d", got, want)
	}
}

func TestToolLifecycleConcurrentReadAndDeferredLoad(t *testing.T) {
	plane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})
	catalog := newLifecycleCatalogForTest(t)

	result, err := plane.Assemble(context.Background(), contextplane.AssembleRequest{
		RunID:       "run-lifecycle-race",
		SessionID:   "sess-lifecycle-race",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), result.LifecycleState, catalog, []*schema.ToolInfo{
		{Name: "read_file"},
		{Name: "mcp.prompt.fetch"},
	})

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 32 {
				_ = contextplane.LoadedToolInfosFromContext(ctx, nil)
			}
		})
	}
	wg.Go(func() {
		if _, err := contextplane.DeferredLoad(ctx, contextplane.DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}}); err != nil {
			t.Errorf("DeferredLoad: %v", err)
		}
	})
	wg.Wait()

	if err := contextplane.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "mcp.prompt.fetch"}); err != nil {
		t.Fatalf("deferred-loaded tool should pass after concurrent reads: %v", err)
	}
}
