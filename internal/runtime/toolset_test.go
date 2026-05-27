package runtime

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
			t.Fatalf("read tool info: %v", err)
		}
		specs[i] = tooling.ToolSpec{
			ToolContract: tooling.ToolContract{
				Name:          info.Name,
				Source:        "test",
				Kind:          tooling.ToolKindNative,
				Category:      tooling.ToolCategoryInspect,
				ResourceScope: tooling.ResourceScopeWorkspaceFile,
				Profiles:      []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe},
				PlanPolicy:    tooling.PlanPolicyNone,
				FactPolicy:    tooling.FactPolicyAuto,
				Loading:       tooling.EagerLoadingPolicy(),
				Execution: tooling.ToolExecutionPolicy{
					ParallelPolicy: tooling.ParallelPolicyReadOnly,
					SideEffects:    []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace},
				},
				Result:     tooling.InlineResultPolicy(0),
				Boundary:   tooling.ToolResultBoundaryPolicy(),
				Projection: tooling.ActivityProjectionPolicy(),
			},
			Tool: tool,
		}
	}
	catalog, err := tooling.NewCatalog(context.Background(), specs)
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

	ts := NewToolset(catalog, tooling.ToolProfileRun)
	all := ts.All()
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
}

func TestToolsetAllForServeProfile(t *testing.T) {
	toolA := buildTestTool(t, "tool_a")
	toolB := buildTestTool(t, "tool_b")
	catalog := buildTestCatalog(t, toolA, toolB)

	ts := NewToolset(catalog, tooling.ToolProfileServe)
	all := ts.All()
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
}

func TestToolsetEmptyCatalog(t *testing.T) {
	ts := NewToolset(nil, tooling.ToolProfileRun)
	if ts.All() != nil {
		t.Fatalf("All() = %v, want nil", ts.All())
	}
	if ts.Catalog() != nil {
		t.Fatalf("Catalog() = %v, want nil", ts.Catalog())
	}
}

func TestToolsetClose(t *testing.T) {
	closer := &testCloser{}
	ts := NewToolset(nil, tooling.ToolProfileRun, closer)
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if !closer.closed {
		t.Fatalf("closer.closed = false, want true")
	}
}

func TestToolsetCloseError(t *testing.T) {
	closerA := &testCloser{err: errors.New("a")}
	closerB := &testCloser{err: errors.New("b")}
	ts := NewToolset(nil, tooling.ToolProfileRun, closerA, closerB)
	err := ts.Close()
	if err == nil {
		t.Fatalf("Close() = nil, want error")
	}
	if !closerA.closed || !closerB.closed {
		t.Fatalf("not all closers were closed")
	}
}

func TestToolsetNilClose(t *testing.T) {
	var ts *Toolset
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() on nil = %v, want nil", err)
	}
}

func TestToolsetNilAll(t *testing.T) {
	var ts Toolset
	if ts.All() != nil {
		t.Fatalf("All() on zero value = %v, want nil", ts.All())
	}
}

func TestToolsetSkipsNilCloser(t *testing.T) {
	closer := &testCloser{}
	ts := NewToolset(nil, tooling.ToolProfileRun, nil, closer, nil)
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if !closer.closed {
		t.Fatalf("closer was not closed")
	}
}

var _ io.Closer = (*testCloser)(nil)
