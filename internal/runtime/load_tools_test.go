package runtime

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	acorntool "github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
)

type stubDeferredPlane struct {
	contextplane.ContextPlane
}

func TestLoadToolsToolCallsDeferredLoad(t *testing.T) {
	baseTool, err := acorntool.NewLoadToolsTool()
	if err != nil {
		t.Fatalf("acorntool.NewLoadToolsTool: %v", err)
	}
	invokable, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("load_tools tool is not invokable")
	}

	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_load_tools"), "run_load_tools")

	catalog, err := tooling.NewCatalog(context.Background(), []tooling.ToolSpec{
		{
			ToolContract: tooling.ToolContract{
				Name:          "memory_search",
				Source:        "test",
				Kind:          tooling.ToolKindNative,
				Category:      tooling.ToolCategoryRead,
				ResourceScope: tooling.ResourceScopeWorkspaceFile,
				Profiles:      []tooling.ToolProfile{tooling.ToolProfileRun},
				PlanPolicy:    tooling.PlanPolicyNone,
				FactPolicy:    tooling.FactPolicyAuto,
				Loading:       tooling.DeferredLoadingPolicy("deferred_catalog"),
				Execution:     tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicyReadOnly},
				Result:        tooling.InlineResultPolicy(0),
				Boundary:      tooling.ToolResultBoundaryPolicy(),
				Projection:    tooling.ActivityProjectionPolicy(),
			},
			Tool: stubTool{name: "memory_search", desc: "Search memory records"},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	state := &contextplane.ToolLifecycleState{
		RunID:         "run_load_tools",
		SessionID:     "sess_load_tools",
		LoadedTools:   map[string]contextplane.LoadedToolRecord{},
		DeferredTools: map[string]contextplane.DeferredToolRecord{"memory_search": {Name: "memory_search", Reason: "test_deferred"}},
	}

	ctx = contextplane.WithToolLifecycleContext(ctx, newMemoryToolResultLedger(), state, catalog, []*schema.ToolInfo{{Name: "memory_search"}})

	result, err := invokable.InvokableRun(ctx, `{"query":"knowledge","tool_names":["memory_search"],"limit":2}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(result, "memory_search") {
		t.Fatalf("unexpected load_tools output: %s", result)
	}

	loaded := contextplane.LoadedToolInfosFromContext(ctx, nil)
	if len(loaded) != 1 || loaded[0].Name != "memory_search" {
		t.Fatalf("expected memory_search to be loaded after call, got %+v", loaded)
	}
}

type stubTool struct {
	name string
	desc string
}

func (t stubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.desc}, nil
}
