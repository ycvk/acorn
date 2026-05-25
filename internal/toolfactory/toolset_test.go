package toolfactory

import (
	"context"
	"errors"
	"io"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/ycvk/acorn/internal/tooling"
)

func buildTestTool(t *testing.T, name string) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, "test tool", func(context.Context, struct{}) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("build test tool %q: %v", name, err)
	}
	return tool
}

func buildTestCatalog(t *testing.T, tools ...einotool.BaseTool) *tooling.Catalog {
	t.Helper()
	specs := make([]tooling.ToolSpec, len(tools))
	for i, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool.Info: %v", err)
		}
		specs[i] = tooling.ToolSpec{
			ToolContract: tooling.ToolContract{
				Name:          info.Name,
				Source:        "test",
				Kind:          tooling.ToolKindNative,
				Category:      tooling.ToolCategoryRead,
				ResourceScope: tooling.ResourceScopeWorkspaceFile,
				PlanPolicy:    tooling.PlanPolicyNone,
				FactPolicy:    tooling.FactPolicyAuto,
				Loading:       tooling.EagerLoadingPolicy(),
				Execution:     tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicyReadOnly},
				Result:        tooling.InlineResultPolicy(0),
				Boundary:      tooling.ToolResultBoundaryPolicy(),
				Projection:    tooling.ActivityProjectionPolicy(),
				Profiles:      []tooling.ToolProfile{tooling.ToolProfileRun},
			},
			Tool: tool,
		}
	}
	catalog, err := tooling.NewCatalog(context.Background(), specs)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func TestToolsetAllNilCatalog(t *testing.T) {
	ts := Toolset{}
	if got := ts.All(); got != nil {
		t.Fatalf("All() = %v, want nil", got)
	}
}

func TestToolsetAll(t *testing.T) {
	tool := buildTestTool(t, "test_tool")
	catalog := buildTestCatalog(t, tool)
	ts := Toolset{catalog: catalog, profile: tooling.ToolProfileRun}

	all := ts.All()
	if len(all) != 1 {
		t.Fatalf("len(All()) = %d, want 1", len(all))
	}
}

func TestToolsetCatalog(t *testing.T) {
	tool := buildTestTool(t, "t")
	catalog := buildTestCatalog(t, tool)
	ts := Toolset{catalog: catalog}

	if got := ts.Catalog(); got != catalog {
		t.Fatalf("Catalog() = %v, want %v", got, catalog)
	}
}

func TestToolsetCloseNil(t *testing.T) {
	var ts *Toolset
	if err := ts.Close(); err != nil {
		t.Fatalf("Close(nil) = %v, want nil", err)
	}
}

func TestToolsetCloseNoClosers(t *testing.T) {
	ts := &Toolset{}
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

func TestToolsetCloseSingleError(t *testing.T) {
	ts := &Toolset{closers: []io.Closer{&errCloser{err: errors.New("boom")}}}
	if err := ts.Close(); err == nil {
		t.Fatalf("Close() = nil, want error")
	}
}

func TestToolsetCloseSkipsNil(t *testing.T) {
	ts := &Toolset{closers: []io.Closer{nil, &okCloser{}}}
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

func TestToolsetCloseReverseOrder(t *testing.T) {
	var order []int
	ts := &Toolset{closers: []io.Closer{
		&orderCloser{n: 1, order: &order},
		&orderCloser{n: 2, order: &order},
	}}
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if len(order) != 2 || order[0] != 2 || order[1] != 1 {
		t.Fatalf("close order = %v, want [2 1]", order)
	}
}

type errCloser struct{ err error }

func (c *errCloser) Close() error { return c.err }

type okCloser struct{}

func (c *okCloser) Close() error { return nil }

type orderCloser struct {
	n     int
	order *[]int
}

func (c *orderCloser) Close() error {
	*c.order = append(*c.order, c.n)
	return nil
}
