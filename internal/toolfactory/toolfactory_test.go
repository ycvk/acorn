package toolfactory

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/tooling"
)

// mockTool is a minimal einotool.BaseTool for testing.
type mockTool struct {
	name string
}

func (m *mockTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: m.name}, nil
}

func (m *mockTool) InvokableRun(ctx context.Context, input string) (string, error) {
	return "", nil
}

func TestRuntimeToolSpec(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := &mockTool{name: "test_tool"}

	spec, err := RuntimeToolSpec(context.Background(), cfg, "native", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, tool)
	if err != nil {
		t.Fatalf("RuntimeToolSpec: %v", err)
	}
	if spec.Name != "test_tool" {
		t.Fatalf("name = %q", spec.Name)
	}
	if spec.Source != "native" {
		t.Fatalf("source = %q", spec.Source)
	}
	if spec.Kind != tooling.ToolKindNative {
		t.Fatalf("kind = %q", spec.Kind)
	}
	if len(spec.Profiles) != 1 || spec.Profiles[0] != tooling.ToolProfileRun {
		t.Fatalf("profiles = %v", spec.Profiles)
	}
}

func TestRuntimeToolSpecEmptyName(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := &mockTool{name: "  "}
	_, err := RuntimeToolSpec(context.Background(), cfg, "native", tooling.ToolKindNative, nil, tool)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestBuildCatalogSpecs(t *testing.T) {
	cfg := config.DefaultConfig()
	tools := []einotool.BaseTool{
		&mockTool{name: "tool_a"},
		&mockTool{name: "tool_b"},
	}

	specs, err := BuildCatalogSpecs(context.Background(), cfg, "native", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, tools)
	if err != nil {
		t.Fatalf("BuildCatalogSpecs: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("len = %d", len(specs))
	}
	if specs[0].Name != "tool_a" || specs[1].Name != "tool_b" {
		t.Fatalf("names = %v", []string{specs[0].Name, specs[1].Name})
	}
}

func TestBuildCatalogSpecsNilTool(t *testing.T) {
	cfg := config.DefaultConfig()
	specs, err := BuildCatalogSpecs(context.Background(), cfg, "native", tooling.ToolKindNative, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalogSpecs with nil: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected 0 specs, got %d", len(specs))
	}
}

func TestNewBuilder(t *testing.T) {
	cfg := config.DefaultConfig()
	b := NewBuilder(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
	if b.cfg != cfg {
		t.Fatal("cfg not set")
	}
}

func TestBuilderBuildNilBuilder(t *testing.T) {
	var b *Builder
	_, err := b.Build(context.Background(), BuildOptions{})
	if err == nil {
		t.Fatal("expected error for nil builder")
	}
}

func TestBuilderBuildNilConfig(t *testing.T) {
	b := &Builder{cfg: nil}
	_, err := b.Build(context.Background(), BuildOptions{})
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestBuilderBuildNilWorkspace(t *testing.T) {
	cfg := config.DefaultConfig()
	b := NewBuilder(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := b.Build(context.Background(), BuildOptions{})
	if err == nil {
		t.Fatal("expected error for nil workspace")
	}
}

func TestNewLoadToolsToolValidation(t *testing.T) {
	_, err := NewLoadToolsTool(nil)
	if err == nil {
		t.Fatal("expected error for nil extractor")
	}
}

func TestBuildMemoryFileToolsNilMemory(t *testing.T) {
	_, err := BuildMemoryFileTools(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil memory")
	}
}
