package tooling

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

func TestNewCatalogNormalizesToolNames(t *testing.T) {
	tool := mustInferTool(t, "local_echo")

	catalog, err := NewCatalog(context.Background(), []ToolSpec{{
		ToolContract: testToolContract("", "local", ParallelPolicyReadOnly),
		Tool:         tool,
	}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	items := catalog.EnabledSpecsForProfile(ToolProfileRun)
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

	_, err := NewCatalog(context.Background(), []ToolSpec{
		{
			ToolContract: testToolContract("", "local", ParallelPolicyReadOnly),
			Tool:         first,
		},
		{
			ToolContract: testToolContract("", "fixture", ParallelPolicyReadOnly),
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
	_, err := NewCatalog(context.Background(), []ToolSpec{{
		ToolContract: testToolContract("read_file", "local", ParallelPolicyReadOnly),
		Health:       healthyTool(""),
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
	catalog, err := NewCatalog(context.Background(), []ToolSpec{{
		ToolContract: testToolContract("", "local", ParallelPolicyReadOnly),
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
	_, err := NewCatalog(context.Background(), []ToolSpec{{
		ToolContract: ToolContract{
			Source:        "local",
			Kind:          ToolKindNative,
			Category:      ToolCategoryRead,
			ResourceScope: ResourceScopeWorkspaceFile,
			Profiles:      []ToolProfile{ToolProfileRun},
			PlanPolicy:    PlanPolicyNone,
			Loading:       EagerLoadingPolicy(),
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

func TestNewCatalogRejectsWriteScopedContractWithoutPathArg(t *testing.T) {
	tool := mustInferTool(t, "create_file")
	contract := testToolContract("create_file", "local", ParallelPolicyWriteScoped)
	contract.Execution.PathArg = ""

	_, err := NewCatalog(context.Background(), []ToolSpec{{
		ToolContract: contract,
		Tool:         tool,
	}})
	if err == nil {
		t.Fatal("expected missing path arg error")
	}
	if !strings.Contains(err.Error(), "write-scoped execution requires path arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testToolContract(name string, source string, parallel ParallelPolicy) ToolContract {
	execution := ToolExecutionPolicy{ParallelPolicy: parallel}
	if parallel == ParallelPolicyWriteScoped {
		execution.PathArg = "path"
	}
	return ToolContract{
		Name:          name,
		Source:        source,
		Kind:          ToolKindNative,
		Category:      ToolCategoryRead,
		ResourceScope: ResourceScopeWorkspaceFile,
		Profiles:      []ToolProfile{ToolProfileRun},
		PlanPolicy:    PlanPolicyNone,
		Loading:       EagerLoadingPolicy(),
		Execution:     execution,
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
