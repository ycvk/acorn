package toollifecycle_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/contextplane/toollifecycle"
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

func testTokenCounter(t *testing.T) *contextplane.CompressionTokenCounter {
	t.Helper()
	counter, err := contextplane.NewCompressionTokenCounter(config.ContextConfig{TokenEncoding: "o200k_base"})
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
		Loading:       loading,
		Execution:     tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicyReadOnly},
	}
}

type fakeContextStore struct {
	records map[string]storerepo.ToolResultRecord
}

func newFakeContextStore() *fakeContextStore {
	return &fakeContextStore{records: make(map[string]storerepo.ToolResultRecord)}
}

func (s *fakeContextStore) Append(_ context.Context, req storerepo.ToolResultAppendRequest) (storerepo.ToolResultRecord, error) {
	ref := "tool_result:" + req.RunID + ":" + req.CallID
	record := storerepo.ToolResultRecord{
		ResultRef:     ref,
		FullText:      req.FullText,
		Preview:       req.FullText,
		ArgumentsJSON: req.ArgumentsJSON,
		TokenEstimate: req.TokenEstimate,
		SideEffects:   append([]storerepo.SideEffectRef(nil), req.SideEffects...),
	}
	s.records[ref] = record
	return record, nil
}

func (s *fakeContextStore) Load(_ context.Context, ref string) (storerepo.ToolResultRecord, error) {
	record, ok := s.records[ref]
	if !ok {
		return storerepo.ToolResultRecord{}, errors.New("not found")
	}
	return record, nil
}

func (s *fakeContextStore) ListByRun(_ context.Context, _ string) ([]storerepo.ToolResultRecord, error) {
	return nil, nil
}

func (s *fakeContextStore) AppendEvidenceRef(_ context.Context, ref string, ev storerepo.EvidenceRef) (storerepo.ToolResultRecord, error) {
	record, ok := s.records[ref]
	if !ok {
		return storerepo.ToolResultRecord{}, errors.New("not found")
	}
	record.EvidenceRefs = append(record.EvidenceRefs, ev)
	s.records[ref] = record
	return record, nil
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
	for _, spec := range catalog.EnabledSpecsForProfile(tooling.ToolProfileRun) {
		info, err := spec.Tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info %s: %v", spec.Name, err)
		}
		toolInfos = append(toolInfos, info)
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), plane.ToolResultLedger(), result.LifecycleState, catalog, toolInfos)

	initial := contextplane.LoadedToolInfosFromContext(ctx, result.EagerToolNames)
	if len(initial) != 1 || initial[0].Name != "read_file" {
		t.Fatalf("initial loaded tool infos = %+v, want only read_file", initial)
	}
	if _, err := toollifecycle.DeferredLoad(ctx, contextplane.DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}}); err != nil {
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
	ctx := contextplane.WithToolLifecycleContext(context.Background(), plane.ToolResultLedger(), result.LifecycleState, catalog, nil)

	if err := toollifecycle.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "read_file"}); err != nil {
		t.Fatalf("loaded tool should pass: %v", err)
	}
	err = toollifecycle.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "mcp.prompt.fetch"})
	var rejected *toollifecycle.ToolCallRejectedError
	if !errors.As(err, &rejected) || rejected.ToolName != "mcp.prompt.fetch" || !strings.Contains(rejected.Reason, "deferred") {
		t.Fatalf("expected deferred tool rejection, got %v", err)
	}
	err = toollifecycle.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "missing_tool"})
	rejected = nil
	if !errors.As(err, &rejected) || rejected.ToolName != "missing_tool" || !strings.Contains(rejected.Reason, "not loaded or enabled") {
		t.Fatalf("expected unknown tool rejection, got %v", err)
	}

	loadResult, err := toollifecycle.DeferredLoad(ctx, contextplane.DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}})
	if err != nil {
		t.Fatalf("DeferredLoad: %v", err)
	}
	if !slices.Equal(loadResult.LoadedToolNames, []string{"mcp.prompt.fetch"}) {
		t.Fatalf("loaded tool names = %+v", loadResult.LoadedToolNames)
	}
	if err := toollifecycle.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "mcp.prompt.fetch"}); err != nil {
		t.Fatalf("deferred-loaded tool should pass: %v", err)
	}
}

func TestDeferredLoadRejectsEmptyRequestAndInvalidLimit(t *testing.T) {
	if _, err := toollifecycle.DeferredLoad(context.Background(), contextplane.DeferredLoadRequest{}); err == nil {
		t.Fatal("expected empty request error")
	}
	if _, err := toollifecycle.DeferredLoad(context.Background(), contextplane.DeferredLoadRequest{
		Query: "git",
		Limit: 6,
	}); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestToolLifecycleEventsRequireToolName(t *testing.T) {
	if err := toollifecycle.OnToolCall(context.Background(), contextplane.ToolCallEvent{}); err == nil {
		t.Fatal("expected tool call validation error")
	}
	if err := toollifecycle.OnToolResult(context.Background(), contextplane.ToolResultEvent{}); err == nil {
		t.Fatal("expected tool result validation error")
	}
}

func TestToolLifecycleEventsRequireStateForNamedTools(t *testing.T) {
	if err := toollifecycle.OnToolCall(context.Background(), contextplane.ToolCallEvent{ToolName: "read_file"}); err == nil || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected lifecycle state runtime error, got %v", err)
	}
	if err := toollifecycle.OnToolResult(context.Background(), contextplane.ToolResultEvent{ToolName: "read_file", CallID: "call_1"}); err == nil || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected lifecycle state runtime error, got %v", err)
	}
}

func TestToolLifecycleOnToolResultPersistsLedgerAndRecentResults(t *testing.T) {
	store := newFakeContextStore()

	state := &contextplane.ToolLifecycleState{
		RunID:         "run_ledger",
		SessionID:     "sess_ledger",
		LoadedTools:   map[string]contextplane.LoadedToolRecord{"read_file": {Name: "read_file"}},
		DeferredTools: map[string]contextplane.DeferredToolRecord{},
		MaxResultRefs: 32,
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), store, state, nil, nil)

	if err := toollifecycle.OnToolResult(ctx, contextplane.ToolResultEvent{
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
	state := &contextplane.ToolLifecycleState{
		RunID:         "run_missing_ledger",
		SessionID:     "sess_missing_ledger",
		LoadedTools:   map[string]contextplane.LoadedToolRecord{"read_file": {Name: "read_file"}},
		DeferredTools: map[string]contextplane.DeferredToolRecord{},
	}
	ctx := contextplane.WithToolLifecycleContext(context.Background(), nil, state, nil, nil)

	err := toollifecycle.OnToolResult(ctx, contextplane.ToolResultEvent{
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
	ctx := contextplane.WithToolLifecycleContext(context.Background(), plane.ToolResultLedger(), result.LifecycleState, catalog, []*schema.ToolInfo{
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
		if _, err := toollifecycle.DeferredLoad(ctx, contextplane.DeferredLoadRequest{ToolNames: []string{"mcp.prompt.fetch"}}); err != nil {
			t.Errorf("DeferredLoad: %v", err)
		}
	})
	wg.Wait()

	if err := toollifecycle.OnToolCall(ctx, contextplane.ToolCallEvent{ToolName: "mcp.prompt.fetch"}); err != nil {
		t.Fatalf("deferred-loaded tool should pass after concurrent reads: %v", err)
	}
}
