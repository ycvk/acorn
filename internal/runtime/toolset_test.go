package runtime

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

type drTestTool struct {
	name string
}

func (t *drTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *drTestTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	return "ok", nil
}

func buildTestTool(t *testing.T, name string) einotool.BaseTool {
	t.Helper()
	return &drTestTool{name: name}
}

func buildTestCatalog(t *testing.T, baseTools ...einotool.BaseTool) *tools.Catalog {
	t.Helper()
	specs := make([]core.ToolSpec, len(baseTools))
	for i, tool := range baseTools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("read tool info: %v", err)
		}
		specs[i] = core.ToolSpec{
			ToolContract: core.ToolContract{
				Name:     info.Name,
				Source:   "test",
				Kind:     core.ToolKindNative,
				Category: core.ToolCategoryInspect,
				Loading:  core.EagerLoadingPolicy(),
			},
			Tool: tool,
		}
	}
	catalog, err := tools.NewCatalog(context.Background(), specs)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	return catalog
}

func TestToolsetCatalog(t *testing.T) {
	catalog := buildTestCatalog(t, buildTestTool(t, "tool_a"), buildTestTool(t, "tool_b"))
	ts := NewToolset(catalog)
	if ts.Catalog() != catalog {
		t.Fatal("Catalog() should return the catalog the toolset was built with")
	}
}
