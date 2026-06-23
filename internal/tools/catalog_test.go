package tools_test

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/tools"
)

func TestNewCatalogNormalizesToolNames(t *testing.T) {
	tool := mustInferTool(t, "local_echo")

	catalog, err := tools.NewCatalog(context.Background(), []tools.ToolSpec{{
		ToolContract: testToolContract("", "local", tools.ParallelPolicyReadOnly),
		Tool:         tool,
	}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	items := catalog.EnabledSpecs()
	if got, want := len(items), 1; got != want {
		t.Fatalf("tool spec count = %d, want %d", got, want)
	}
	if got, want := items[0].Name, "local_echo"; got != want {
		t.Fatalf("tool spec name = %q, want %q", got, want)
	}
}

func TestNewCatalogRejectsDuplicateNames(t *testing.T) {
	first := mustInferTool(t, "shared_name")
	second := mustInferTool(t, "shared_name")

	_, err := tools.NewCatalog(context.Background(), []tools.ToolSpec{
		{
			ToolContract: testToolContract("", "local", tools.ParallelPolicyReadOnly),
			Tool:         first,
		},
		{
			ToolContract: testToolContract("", "fixture", tools.ParallelPolicyReadOnly),
			Tool:         second,
		},
	})
	if err == nil {
		t.Fatal("expected duplicate tool spec error")
	}
	if !strings.Contains(err.Error(), "duplicate capability name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewCatalogRejectsEnabledSpecWithoutTool(t *testing.T) {
	_, err := tools.NewCatalog(context.Background(), []tools.ToolSpec{{
		ToolContract: testToolContract("read_file", "local", tools.ParallelPolicyReadOnly),
		Health:       tools.ToolHealth{State: tools.HealthStateHealthy},
	}})
	if err == nil {
		t.Fatal("expected missing tool error")
	}
	if !strings.Contains(err.Error(), "missing tool implementation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCatalogExecutionPolicyRejectsUnknownTool(t *testing.T) {
	tool := mustInferTool(t, "local_echo")
	catalog, err := tools.NewCatalog(context.Background(), []tools.ToolSpec{{
		ToolContract: testToolContract("", "local", tools.ParallelPolicyReadOnly),
		Tool:         tool,
	}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	if _, err := catalog.ExecutionPolicy("missing_tool", nil); err == nil {
		t.Fatal("expected unknown execution policy error")
	}
}

func TestNewCatalogRejectsIncompleteContract(t *testing.T) {
	tool := mustInferTool(t, "local_echo")
	_, err := tools.NewCatalog(context.Background(), []tools.ToolSpec{{
		ToolContract: tools.ToolContract{
			Source:   "local",
			Kind:     tools.ToolKindNative,
			Category: tools.ToolCategoryRead,
			Loading:  tools.EagerLoadingPolicy(),
		},
		Tool: tool,
	}})
	if err == nil {
		t.Fatal("expected incomplete contract error")
	}
	if !strings.Contains(err.Error(), "unknown parallel policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testToolContract(name string, source string, parallel tools.ParallelPolicy) tools.ToolContract {
	execution := tools.ToolExecutionPolicy{ParallelPolicy: parallel}
	return tools.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      tools.ToolKindNative,
		Category:  tools.ToolCategoryRead,
		Loading:   tools.EagerLoadingPolicy(),
		Execution: execution,
	}
}

func mustInferTool(t *testing.T, name string) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("infer tool: %v", err)
	}
	return tool
}
