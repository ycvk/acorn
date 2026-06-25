package runtime

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

// stubTool is a minimal einotool.BaseTool that reports a fixed name.
type stubTool struct{ name string }

func (t *stubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *stubTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "", nil
}

// TestBuildCoreToolSpecsExcludesEagerNatives verifies that buildCoreToolSpecs
// only produces deferred-loaded native tools + memory + skill specs. Eager
// native tools are owned by the registry and must NOT appear in the toolset
// catalog — otherwise they would be built twice.
func TestBuildCoreToolSpecsExcludesEagerNatives(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}

	// Simulate a local catalog containing both eager and deferred tool
	// instances. In production, BuildCatalog produces these from per-run
	// services.
	eagerTool := &stubTool{name: "read_file"}
	deferredTool := &stubTool{name: "web_fetch"}
	localCatalog := &tools.LocalCatalog{
		Tools: []einotool.BaseTool{eagerTool, deferredTool},
	}

	specs, err := buildCoreToolSpecs(ctx, cfg, localCatalog, auxTools{})
	if err != nil {
		t.Fatalf("buildCoreToolSpecs: %v", err)
	}

	for _, spec := range specs {
		if spec.Source == "local" && spec.Kind == core.ToolKindNative {
			if spec.Loading.Mode != core.ToolLoadingModeDeferred {
				t.Errorf("eager native tool %q leaked into toolset catalog specs (loading=%s); only deferred natives should appear",
					spec.Name, spec.Loading.Mode)
			}
		}
	}

	// The deferred tool must be present; the eager tool must be absent.
	byName := make(map[string]core.ToolSpec, len(specs))
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	if _, ok := byName["web_fetch"]; !ok {
		t.Errorf("deferred native tool web_fetch missing from toolset catalog specs")
	}
	if _, ok := byName["read_file"]; ok {
		t.Errorf("eager native tool read_file should not be in toolset catalog specs")
	}
}

// TestRegisterNativeToolsExcludesDeferred verifies that RegisterNativeTools
// only registers eager-loaded tools. Deferred-loaded tools (web/browser) are
// per-run and must not be in the wire-time registry.
func TestRegisterNativeToolsExcludesDeferred(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := tools.RegisterNativeTools(registry, tools.CatalogConfig{}); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}

	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		if _, ok := registry.Find(name); ok {
			t.Errorf("deferred tool %q should not be registered in the wire-time registry", name)
		}
	}

	// Eager natives must be present.
	for _, name := range []string{"read_file", "list_files", "git_summary"} {
		if _, ok := registry.Find(name); !ok {
			t.Errorf("eager native tool %q missing from registry", name)
		}
	}
}
