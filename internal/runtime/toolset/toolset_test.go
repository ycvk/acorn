package toolset

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/toolkit"
)

type testTool struct {
	name string
}

func (t *testTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *testTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	return "ok", nil
}

func buildTestTool(t *testing.T, name string) einotool.BaseTool {
	t.Helper()
	return &testTool{name: name}
}

func buildTestCatalog(t *testing.T, tools ...einotool.BaseTool) *toolkit.Catalog {
	t.Helper()
	specs := make([]toolkit.ToolSpec, len(tools))
	for i, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("read tool info: %v", err)
		}
		specs[i] = toolkit.ToolSpec{
			ToolContract: toolkit.ToolContract{
				Name:      info.Name,
				Source:    "test",
				Kind:      toolkit.ToolKindNative,
				Category:  toolkit.ToolCategoryInspect,
				Loading:   toolkit.EagerLoadingPolicy(),
				Execution: toolkit.ToolExecutionPolicy{ParallelPolicy: toolkit.ParallelPolicyReadOnly},
			},
			Tool: tool,
		}
	}
	catalog, err := toolkit.NewCatalog(context.Background(), specs)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	return catalog
}

type testCloser struct {
	closed bool
	err    error
}

func (c *testCloser) Close() error {
	c.closed = true
	return c.err
}

func TestToolsetAll(t *testing.T) {
	toolA := buildTestTool(t, "tool_a")
	toolB := buildTestTool(t, "tool_b")
	catalog := buildTestCatalog(t, toolA, toolB)

	ts := NewToolset(catalog)
	all := ts.All()
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
}

func TestToolsetEmptyCatalog(t *testing.T) {
	ts := NewToolset(nil)
	if ts.All() != nil {
		t.Fatalf("All() = %v, want nil", ts.All())
	}
}

func TestToolsetClosesClosers(t *testing.T) {
	closer := &testCloser{}
	ts := NewToolset(buildTestCatalog(t, buildTestTool(t, "tool_a")), closer)
	if err := ts.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !closer.closed {
		t.Fatal("closer was not closed")
	}
}
