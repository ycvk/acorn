package runtime

import (
	"context"
	"slices"
	"testing"

	"github.com/cloudwego/eino/schema"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

type lifecycleStubTool struct {
	name string
	desc string
}

func (t lifecycleStubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.desc}, nil
}
func (t lifecycleStubTool) InvokableRun(context.Context, string) (string, error) {
	return "ok", nil
}

// stubCatalog implements core.Catalog for testing without importing tools package.
type stubCatalog struct {
	specs []core.ToolSpec
}

func (c *stubCatalog) Specs() []core.ToolSpec { return c.specs }
func (c *stubCatalog) EnabledSpecs() []core.ToolSpec {
	out := make([]core.ToolSpec, 0, len(c.specs))
	for _, s := range c.specs {
		if s.Enabled() {
			out = append(out, s)
		}
	}
	return out
}
func (c *stubCatalog) Tools() []einotool.BaseTool {
	out := make([]einotool.BaseTool, 0, len(c.specs))
	for _, s := range c.specs {
		if s.Tool != nil {
			out = append(out, s.Tool)
		}
	}
	return out
}
func (c *stubCatalog) Find(name string) (core.ToolSpec, bool) {
	for _, s := range c.specs {
		if s.Name == name {
			return s, true
		}
	}
	return core.ToolSpec{}, false
}
func (c *stubCatalog) ExecutionPolicy(toolName string, args map[string]any) (core.ToolExecutionPolicy, error) {
	s, ok := c.Find(toolName)
	if !ok {
		return core.ToolExecutionPolicy{}, nil
	}
	return s.Execution, nil
}

var _ core.Catalog = (*stubCatalog)(nil)

func newLifecycleCatalogForTest() *stubCatalog {
	return &stubCatalog{
		specs: []core.ToolSpec{
			{
				ToolContract: lifecycleToolContract("read_file", "local", core.ToolKindNative, core.EagerLoadingPolicy()),
				Tool:         lifecycleStubTool{name: "read_file", desc: "Read a file"},
				Health:       core.ToolHealth{State: core.HealthStateHealthy},
			},
			{
				ToolContract: lifecycleToolContract("mcp.prompt.fetch", "mcp.prompt", core.ToolKindMCP, core.DeferredLoadingPolicy("deferred_mcp_catalog")),
				Tool:         lifecycleStubTool{name: "mcp.prompt.fetch", desc: "Fetch MCP prompt"},
				Health:       core.ToolHealth{State: core.HealthStateHealthy},
			},
		},
	}
}

func lifecycleToolContract(name string, source string, kind core.ToolKind, loading core.ToolLoadingPolicy) core.ToolContract {
	return core.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      kind,
		Category:  core.ToolCategoryRead,
		Loading:   loading,
		Execution: core.ToolExecutionPolicy{ParallelPolicy: core.ParallelPolicyReadOnly},
	}
}

func TestDefaultContextPlaneAssembleBuildsLifecycleToolSplit(t *testing.T) {
	plane := NewDefaultPlane(DefaultOptions{
		MemoryContextTokenBudget: 100,
		TokenCounter:             testTokenCounter(t),
	})

	catalog := newLifecycleCatalogForTest()

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
