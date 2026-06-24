package tools_test

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/port"

	"github.com/ycvk/acorn/internal/tools"
)

func TestNewCatalogNormalizesToolNames(t *testing.T) {
	tool := mustInferTool(t, "local_echo")

	catalog, err := tools.NewCatalog(context.Background(), []port.ToolSpec{{
		ToolContract: testToolContract("", "local", port.ParallelPolicyReadOnly),
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

	_, err := tools.NewCatalog(context.Background(), []port.ToolSpec{
		{
			ToolContract: testToolContract("", "local", port.ParallelPolicyReadOnly),
			Tool:         first,
		},
		{
			ToolContract: testToolContract("", "fixture", port.ParallelPolicyReadOnly),
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
	_, err := tools.NewCatalog(context.Background(), []port.ToolSpec{{
		ToolContract: testToolContract("read_file", "local", port.ParallelPolicyReadOnly),
		Health:       port.ToolHealth{State: port.HealthStateHealthy},
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
	catalog, err := tools.NewCatalog(context.Background(), []port.ToolSpec{{
		ToolContract: testToolContract("", "local", port.ParallelPolicyReadOnly),
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
	_, err := tools.NewCatalog(context.Background(), []port.ToolSpec{{
		ToolContract: port.ToolContract{
			Source:   "local",
			Kind:     port.ToolKindNative,
			Category: port.ToolCategoryRead,
			Loading:  port.EagerLoadingPolicy(),
		},
		Tool: tool,
	}})
	if err == nil {
		t.Fatal("expected incomplete contract error")
	}
	if !strings.Contains(err.Error(), "parallel policy is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testToolContract(name string, source string, parallel port.ParallelPolicy) port.ToolContract {
	execution := port.ToolExecutionPolicy{ParallelPolicy: parallel}
	return port.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      port.ToolKindNative,
		Category:  port.ToolCategoryRead,
		Loading:   port.EagerLoadingPolicy(),
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
