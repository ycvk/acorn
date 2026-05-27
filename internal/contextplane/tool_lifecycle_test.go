package contextplane

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/model"
	storerepo "github.com/ycvk/acorn/internal/store"
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

func newLifecycleCatalogForTest(t *testing.T) *tooling.Catalog {
	t.Helper()
	catalog, err := tooling.NewCatalog(context.Background(), []tooling.ToolSpec{
		{
			ToolContract: lifecycleToolContract("read_file", "local", tooling.ToolKindNative, tooling.EagerLoadingPolicy()),
			Tool:         lifecycleStubTool{name: "read_file", desc: "Read a file"},
			Health:       tooling.ToolHealth{State: tooling.HealthStateHealthy},
		},
		{
			ToolContract: lifecycleToolContract("mcp.prompt.fetch", "mcp.prompt", tooling.ToolKindMCPPrompt, tooling.DeferredLoadingPolicy("deferred_mcp_catalog")),
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
		Name:          name,
		Source:        source,
		Kind:          kind,
		Category:      tooling.ToolCategoryRead,
		ResourceScope: tooling.ResourceScopeWorkspaceFile,
		Profiles:      []tooling.ToolProfile{tooling.ToolProfileRun},
		PlanPolicy:    tooling.PlanPolicyNone,
		FactPolicy:    tooling.FactPolicyAuto,
		Loading:       loading,
		Execution:     tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicyReadOnly},
		Result:        tooling.InlineResultPolicy(0),
		Boundary:      tooling.ToolResultBoundaryPolicy(),
		Projection:    tooling.ActivityProjectionPolicy(),
	}
}

func TestDefaultContextPlaneAssembleBuildsLifecycleToolSplit(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})

	catalog := newLifecycleCatalogForTest(t)

	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:       "run-lifecycle",
		SessionID:   "sess-lifecycle",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if result.LifecycleState == nil {
		t.Fatal("expected lifecycle state")
	}
	if !slices.Equal(result.EagerToolNames, []string{"read_file"}) {
		t.Fatalf("eager tool names = %+v", result.EagerToolNames)
	}
	if !slices.Equal(result.DeferredToolNames, []string{"mcp.prompt.fetch"}) {
		t.Fatalf("deferred tool names = %+v", result.DeferredToolNames)
	}
	if _, ok := result.LifecycleState.LoadedTools["read_file"]; !ok {
		t.Fatalf("loaded tools missing read_file: %+v", result.LifecycleState.LoadedTools)
	}
	if _, ok := result.LifecycleState.DeferredTools["mcp.prompt.fetch"]; !ok {
		t.Fatalf("deferred tools missing mcp.prompt.fetch: %+v", result.LifecycleState.DeferredTools)
	}
}

func TestLoadedToolInfosFromContextExcludesDeferredUntilLoaded(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})
	catalog := newLifecycleCatalogForTest(t)
	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:       "run-lifecycle-infos",
		SessionID:   "sess-lifecycle-infos",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	var toolInfos []*schema.ToolInfo
	for _, spec := range catalog.EnabledSpecsForProfile(tooling.ToolProfileRun) {
		info, err := spec.Tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info %s: %v", spec.Name, err)
		}
		toolInfos = append(toolInfos, info)
	}
	ctx := WithToolLifecycleContext(context.Background(), plane, result.LifecycleState, catalog, toolInfos)

	initial := LoadedToolInfosFromContext(ctx, result.EagerToolNames)
	if len(initial) != 1 || initial[0].Name != "read_file" {
		t.Fatalf("initial loaded tool infos = %+v, want only read_file", initial)
	}
	if _, err := plane.DeferredLoad(ctx, DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}}); err != nil {
		t.Fatalf("DeferredLoad: %v", err)
	}
	loaded := LoadedToolInfosFromContext(ctx, result.EagerToolNames)
	if len(loaded) != 2 || loaded[0].Name != "mcp.prompt.fetch" || loaded[1].Name != "read_file" {
		t.Fatalf("loaded tool infos = %+v, want deferred tool after load plus read_file", loaded)
	}
}

func TestToolLifecycleOnToolCallRequiresLoadedTool(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})
	catalog := newLifecycleCatalogForTest(t)

	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:       "run-lifecycle-call",
		SessionID:   "sess-lifecycle-call",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	ctx := WithToolLifecycleContext(context.Background(), plane, result.LifecycleState, catalog, nil)

	if err := plane.OnToolCall(ctx, ToolCallEvent{ToolName: "read_file"}); err != nil {
		t.Fatalf("loaded tool should pass: %v", err)
	}
	err = plane.OnToolCall(ctx, ToolCallEvent{ToolName: "mcp.prompt.fetch"})
	var rejected *ToolCallRejectedError
	if !errors.As(err, &rejected) || rejected.ToolName != "mcp.prompt.fetch" || !strings.Contains(rejected.Reason, "deferred") {
		t.Fatalf("expected deferred tool rejection, got %v", err)
	}
	err = plane.OnToolCall(ctx, ToolCallEvent{ToolName: "missing_tool"})
	rejected = nil
	if !errors.As(err, &rejected) || rejected.ToolName != "missing_tool" || !strings.Contains(rejected.Reason, "not loaded or enabled") {
		t.Fatalf("expected unknown tool rejection, got %v", err)
	}

	loadResult, err := plane.DeferredLoad(ctx, DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}})
	if err != nil {
		t.Fatalf("DeferredLoad: %v", err)
	}
	if !slices.Equal(loadResult.LoadedToolNames, []string{"mcp.prompt.fetch"}) {
		t.Fatalf("loaded tool names = %+v", loadResult.LoadedToolNames)
	}
	if err := plane.OnToolCall(ctx, ToolCallEvent{ToolName: "mcp.prompt.fetch"}); err != nil {
		t.Fatalf("deferred-loaded tool should pass: %v", err)
	}
}

func TestDeferredLoadRejectsEmptyRequestAndInvalidLimit(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{})

	if _, err := plane.DeferredLoad(context.Background(), DeferredLoadRequest{}); err == nil {
		t.Fatal("expected empty request error")
	}
	if _, err := plane.DeferredLoad(context.Background(), DeferredLoadRequest{
		Query: "git",
		Limit: 6,
	}); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestToolLifecycleEventsRequireToolName(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{})

	if err := plane.OnToolCall(context.Background(), ToolCallEvent{}); err == nil {
		t.Fatal("expected tool call validation error")
	}
	if err := plane.OnToolResult(context.Background(), ToolResultEvent{}); err == nil {
		t.Fatal("expected tool result validation error")
	}
}

func TestToolLifecycleEventsRequireStateForNamedTools(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{})

	if err := plane.OnToolCall(context.Background(), ToolCallEvent{ToolName: "read_file"}); err == nil || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected lifecycle state runtime error, got %v", err)
	}
	if err := plane.OnToolResult(context.Background(), ToolResultEvent{ToolName: "read_file", CallID: "call_1"}); err == nil || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected lifecycle state runtime error, got %v", err)
	}
}

func TestToolLifecycleOnToolResultPersistsLedgerAndRecentResults(t *testing.T) {
	store := newFakeContextStore()

	plane := NewDefaultContextPlane(DefaultOptions{ToolResultLedger: store})
	state := &ToolLifecycleState{
		RunID:         "run_ledger",
		SessionID:     "sess_ledger",
		LoadedTools:   map[string]LoadedToolRecord{"read_file": {Name: "read_file"}},
		DeferredTools: map[string]DeferredToolRecord{},
		MaxResultRefs: 32,
	}
	ctx := WithToolLifecycleContext(context.Background(), plane, state, nil, nil)

	if err := plane.OnToolResult(ctx, ToolResultEvent{
		RunID:        "run_ledger",
		SessionID:    "sess_ledger",
		TurnIndex:    3,
		CallID:       "call_1",
		ToolName:     "read_file",
		Arguments:    `{"path":"README.md"}`,
		Result:       "tool output body",
		ResultTokens: 4,
		SideEffects: []storerepo.SideEffectRef{{
			Kind: "workspace_read",
			Path: "README.md",
		}},
	}); err != nil {
		t.Fatalf("OnToolResult: %v", err)
	}

	if len(state.RecentResults) != 1 {
		t.Fatalf("recent result count = %d, want 1", len(state.RecentResults))
	}
	if got, want := state.RecentResults[0].ResultRef, "tool_result:run_ledger:call_1"; got != want {
		t.Fatalf("recent result ref = %q, want %q", got, want)
	}
	if got, want := state.RecentResults[0].Summary, "tool output body"; got != want {
		t.Fatalf("recent result summary = %q, want %q", got, want)
	}

	record, err := store.Load(context.Background(), "tool_result:run_ledger:call_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := record.FullText, "tool output body"; got != want {
		t.Fatalf("ledger full text = %q, want %q", got, want)
	}
	if got, want := record.Preview, "tool output body"; got != want {
		t.Fatalf("ledger preview = %q, want %q", got, want)
	}
	if got, want := record.ArgumentsJSON, `{"path":"README.md"}`; got != want {
		t.Fatalf("ledger arguments json = %q, want %q", got, want)
	}
	if got, want := record.TokenEstimate, 4; got != want {
		t.Fatalf("ledger token estimate = %d, want %d", got, want)
	}
	if len(record.SideEffects) != 1 || record.SideEffects[0].Kind != "workspace_read" || record.SideEffects[0].Path != "README.md" {
		t.Fatalf("ledger side effects = %+v", record.SideEffects)
	}
}

func TestToolLifecycleOnToolResultRequiresLedger(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{})
	state := &ToolLifecycleState{
		RunID:         "run_missing_ledger",
		SessionID:     "sess_missing_ledger",
		LoadedTools:   map[string]LoadedToolRecord{"read_file": {Name: "read_file"}},
		DeferredTools: map[string]DeferredToolRecord{},
	}
	ctx := WithToolLifecycleContext(context.Background(), plane, state, nil, nil)

	err := plane.OnToolResult(ctx, ToolResultEvent{
		RunID:     "run_missing_ledger",
		SessionID: "sess_missing_ledger",
		CallID:    "call_1",
		ToolName:  "read_file",
		Result:    "tool output body",
	})
	if err == nil || !strings.Contains(err.Error(), "tool result ledger is not initialized") {
		t.Fatalf("expected missing ledger error, got %v", err)
	}
}

func TestToolLifecycleConcurrentReadAndDeferredLoad(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
		Store:                    lifecycleSnapshotStore{},
	})
	catalog := newLifecycleCatalogForTest(t)

	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:       "run-lifecycle-race",
		SessionID:   "sess-lifecycle-race",
		Input:       "inspect repo",
		ToolCatalog: catalog,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	ctx := WithToolLifecycleContext(context.Background(), plane, result.LifecycleState, catalog, []*schema.ToolInfo{
		{Name: "read_file"},
		{Name: "mcp.prompt.fetch"},
	})

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 32; i++ {
				_ = LoadedToolInfosFromContext(ctx, nil)
				_ = sortedLoadedToolNames(result.LifecycleState)
				_ = sortedDeferredToolNames(result.LifecycleState)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := plane.DeferredLoad(ctx, DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}}); err != nil {
			t.Errorf("DeferredLoad: %v", err)
		}
	}()
	wg.Wait()

	if err := plane.OnToolCall(ctx, ToolCallEvent{ToolName: "mcp.prompt.fetch"}); err != nil {
		t.Fatalf("deferred-loaded tool should pass after concurrent reads: %v", err)
	}
}
