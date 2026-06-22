package contextplane

import (
	"context"
	"slices"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/toolkit"
)

type lifecycleStubTool struct {
	name string
	desc string
}

func (t lifecycleStubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.desc}, nil
}

func newLifecycleCatalogForTest(t *testing.T) *toolkit.Catalog {
	t.Helper()
	catalog, err := toolkit.NewCatalog(context.Background(), []toolkit.ToolSpec{
		{
			ToolContract: lifecycleToolContract("read_file", "local", toolkit.ToolKindNative, toolkit.EagerLoadingPolicy()),
			Tool:         lifecycleStubTool{name: "read_file", desc: "Read a file"},
			Health:       toolkit.ToolHealth{State: toolkit.HealthStateHealthy},
		},
		{
			ToolContract: lifecycleToolContract("mcp.prompt.fetch", "mcp.prompt", toolkit.ToolKindMCP, toolkit.DeferredLoadingPolicy("deferred_mcp_catalog")),
			Tool:         lifecycleStubTool{name: "mcp.prompt.fetch", desc: "Fetch MCP prompt"},
			Health:       toolkit.ToolHealth{State: toolkit.HealthStateHealthy},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func lifecycleToolContract(name string, source string, kind toolkit.ToolKind, loading toolkit.ToolLoadingPolicy) toolkit.ToolContract {
	return toolkit.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      kind,
		Category:  toolkit.ToolCategoryRead,
		Loading:   loading,
		Execution: toolkit.ToolExecutionPolicy{ParallelPolicy: toolkit.ParallelPolicyReadOnly},
	}
}

func TestDefaultContextPlaneAssembleBuildsLifecycleToolSplit(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
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
