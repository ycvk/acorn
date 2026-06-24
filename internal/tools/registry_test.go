package tools_test

import (
	"context"
	"errors"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/core"

	"github.com/ycvk/acorn/internal/tools"
)

// mustInferCoreTool builds a minimal einotool.BaseTool suitable for the
// registry tests. It mirrors the helper in catalog_test.go but lives locally
// so registry_test.go stays self-contained.
func mustInferCoreTool(t *testing.T, name string) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("infer tool: %v", err)
	}
	return tool
}

// coreToolContract builds a valid core.ToolContract for tests.
func coreToolContract(name string, parallel core.ParallelPolicy) core.ToolContract {
	return core.ToolContract{
		Name:      name,
		Source:    "local",
		Kind:      core.ToolKindNative,
		Category:  core.ToolCategoryRead,
		Loading:   core.EagerLoadingPolicy(),
		Execution: core.ToolExecutionPolicy{ParallelPolicy: parallel},
	}
}

func TestToolRegistryRegisterAndList(t *testing.T) {
	reg := tools.NewToolRegistry()
	tool := mustInferCoreTool(t, "local_echo")

	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicyReadOnly),
		Tool:         tool,
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}

	specs := reg.Specs()
	if got, want := len(specs), 1; got != want {
		t.Fatalf("Specs count = %d, want %d", got, want)
	}
	if got, want := specs[0].Name, "local_echo"; got != want {
		t.Fatalf("Specs[0].Name = %q, want %q", got, want)
	}

	// Enabled spec should surface because Health defaults to healthy via
	// normalizeCoreSpec when a Tool is present.
	enabled := reg.EnabledSpecs()
	if got, want := len(enabled), 1; got != want {
		t.Fatalf("EnabledSpecs count = %d, want %d", got, want)
	}

	// Tools() returns the pre-built instance.
	toolsList := reg.Tools()
	if got, want := len(toolsList), 1; got != want {
		t.Fatalf("Tools count = %d, want %d", got, want)
	}

	// Find locates the registered spec.
	if found, ok := reg.Find("local_echo"); !ok {
		t.Fatalf("Find: spec not found")
	} else if got, want := found.Name, "local_echo"; got != want {
		t.Fatalf("Find.Name = %q, want %q", got, want)
	}
}

func TestToolRegistryRejectsDuplicate(t *testing.T) {
	reg := tools.NewToolRegistry()
	tool := mustInferCoreTool(t, "local_echo")
	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicyReadOnly),
		Tool:         tool,
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register first: %v", err)
	}
	if err := reg.Register(spec); err == nil {
		t.Fatalf("Register duplicate: expected error, got nil")
	}
}

func TestToolRegistryRejectsEmptyName(t *testing.T) {
	reg := tools.NewToolRegistry()
	spec := core.ToolSpec{
		ToolContract: coreToolContract("", core.ParallelPolicyReadOnly),
		Tool:         mustInferCoreTool(t, "anon"),
	}
	if err := reg.Register(spec); err == nil {
		t.Fatalf("Register empty name: expected error, got nil")
	}
}

func TestToolRegistryUnregister(t *testing.T) {
	reg := tools.NewToolRegistry()
	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicyReadOnly),
		Tool:         mustInferCoreTool(t, "local_echo"),
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Unregister("local_echo"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if got := len(reg.Specs()); got != 0 {
		t.Fatalf("Specs count after unregister = %d, want 0", got)
	}
	if _, ok := reg.Find("local_echo"); ok {
		t.Fatalf("Find after unregister: still found")
	}
	// Unregister again is an error.
	if err := reg.Unregister("local_echo"); err == nil {
		t.Fatalf("Unregister unknown: expected error, got nil")
	}
}

func TestToolRegistryResolveFactory(t *testing.T) {
	reg := tools.NewToolRegistry()
	tool := mustInferCoreTool(t, "local_echo")

	// Factory returns the pre-built tool; callCount verifies it was invoked.
	callCount := 0
	factory := func(ctx context.Context, runCtx core.RunContext) (einotool.BaseTool, error) {
		callCount++
		return tool, nil
	}
	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicyReadOnly),
		Factory:      factory,
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}

	runCtx := core.RunContext{RunID: "r1", SessionID: "s1", TurnIndex: 0}
	got, err := reg.Resolve(context.Background(), runCtx, []string{"local_echo"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := len(got), 1; got != want {
		t.Fatalf("Resolve count = %d, want %d", got, want)
	}
	if callCount != 1 {
		t.Fatalf("factory call count = %d, want 1", callCount)
	}
}

func TestToolRegistryResolveUnknownSkipped(t *testing.T) {
	reg := tools.NewToolRegistry()
	tool := mustInferCoreTool(t, "local_echo")
	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicyReadOnly),
		Factory: func(ctx context.Context, runCtx core.RunContext) (einotool.BaseTool, error) {
			return tool, nil
		},
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Unknown names are skipped, not errors.
	got, err := reg.Resolve(context.Background(), core.RunContext{}, []string{"local_echo", "does_not_exist"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := len(got), 1; got != want {
		t.Fatalf("Resolve count with unknown = %d, want %d (unknown skipped)", got, want)
	}
}

func TestToolRegistryResolveFactoryError(t *testing.T) {
	reg := tools.NewToolRegistry()
	boom := errors.New("boom")
	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicyReadOnly),
		Factory: func(ctx context.Context, runCtx core.RunContext) (einotool.BaseTool, error) {
			return nil, boom
		},
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Resolve(context.Background(), core.RunContext{}, []string{"local_echo"}); err == nil {
		t.Fatalf("Resolve with failing factory: expected error, got nil")
	}
}

func TestToolRegistryExecutionPolicy(t *testing.T) {
	reg := tools.NewToolRegistry()
	spec := core.ToolSpec{
		ToolContract: coreToolContract("local_echo", core.ParallelPolicySerial),
		Tool:         mustInferCoreTool(t, "local_echo"),
	}
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	pol, err := reg.ExecutionPolicy("local_echo", nil)
	if err != nil {
		t.Fatalf("ExecutionPolicy: %v", err)
	}
	if got, want := pol.ParallelPolicy, core.ParallelPolicySerial; got != want {
		t.Fatalf("ExecutionPolicy.ParallelPolicy = %q, want %q", got, want)
	}
	if _, err := reg.ExecutionPolicy("missing", nil); err == nil {
		t.Fatalf("ExecutionPolicy for unknown: expected error, got nil")
	}
}
