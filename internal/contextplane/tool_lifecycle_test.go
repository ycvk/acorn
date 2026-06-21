package contextplane

import (
	"context"
	"slices"
	"testing"

	"github.com/cloudwego/eino/schema"
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
