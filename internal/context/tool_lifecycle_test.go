package context

import (
	"context"
	"slices"
	"testing"

	"github.com/cloudwego/eino/schema"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/port"
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

// stubCatalog implements port.Catalog for testing without importing tools package.
type stubCatalog struct {
	specs []port.ToolSpec
}

func (c *stubCatalog) Specs() []port.ToolSpec { return c.specs }
func (c *stubCatalog) EnabledSpecs() []port.ToolSpec {
	out := make([]port.ToolSpec, 0, len(c.specs))
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
func (c *stubCatalog) Find(name string) (port.ToolSpec, bool) {
	for _, s := range c.specs {
		if s.Name == name {
			return s, true
		}
	}
	return port.ToolSpec{}, false
}
func (c *stubCatalog) ExecutionPolicy(toolName string, args map[string]any) (port.ToolExecutionPolicy, error) {
	s, ok := c.Find(toolName)
	if !ok {
		return port.ToolExecutionPolicy{}, nil
	}
	return s.Execution, nil
}

var _ port.Catalog = (*stubCatalog)(nil)

func newLifecycleCatalogForTest() *stubCatalog {
	return &stubCatalog{
		specs: []port.ToolSpec{
			{
				ToolContract: lifecycleToolContract("read_file", "local", port.ToolKindNative, port.EagerLoadingPolicy()),
				Tool:         lifecycleStubTool{name: "read_file", desc: "Read a file"},
				Health:       port.ToolHealth{State: port.HealthStateHealthy},
			},
			{
				ToolContract: lifecycleToolContract("mcp.prompt.fetch", "mcp.prompt", port.ToolKindMCP, port.DeferredLoadingPolicy("deferred_mcp_catalog")),
				Tool:         lifecycleStubTool{name: "mcp.prompt.fetch", desc: "Fetch MCP prompt"},
				Health:       port.ToolHealth{State: port.HealthStateHealthy},
			},
		},
	}
}

func lifecycleToolContract(name string, source string, kind port.ToolKind, loading port.ToolLoadingPolicy) port.ToolContract {
	return port.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      kind,
		Category:  port.ToolCategoryRead,
		Loading:   loading,
		Execution: port.ToolExecutionPolicy{ParallelPolicy: port.ParallelPolicyReadOnly},
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
