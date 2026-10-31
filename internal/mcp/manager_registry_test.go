package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func TestManagerFailsAProviderWhoseToolsCannotRegister(t *testing.T) {
	binary := buildFixtureServer(t)
	registry := tools.NewToolRegistry()
	builder := func(ctx context.Context, providerName string, tool einotool.BaseTool) (core.ToolSpec, error) {
		info, err := tool.Info(ctx)
		if err != nil {
			return core.ToolSpec{}, err
		}
		if providerName == "bad" {
			return core.ToolSpec{}, errors.New("tool is not invokable")
		}
		spec := core.ToolSpec{Tool: tool}
		spec.Name, spec.Kind, spec.Category = "mcp__"+providerName+"__"+info.Name, core.ToolKindMCP, core.ToolCategoryIntegration
		spec.Loading.Mode = core.ToolLoadingModeEager
		return spec, nil
	}
	mgr, err := NewManager(context.Background(), []core.ProviderConfig{
		{Name: "good", Enabled: true, Transport: "stdio", Command: binary, StartupTimeoutSeconds: 10},
		{Name: "bad", Enabled: true, Transport: "stdio", Command: binary, StartupTimeoutSeconds: 10},
	}, WithToolRegistry(registry, builder))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	var good, bad int
	for _, spec := range registry.Specs() {
		switch {
		case strings.HasPrefix(spec.Name, "mcp__good__"):
			good++
		case strings.HasPrefix(spec.Name, "mcp__bad__"):
			bad++
		}
	}
	if good == 0 || bad != 0 {
		t.Fatalf("registered good=%d bad=%d", good, bad)
	}
	if got := len(mgr.Resources()); got != 1 {
		t.Fatalf("resources = %d, want only the good provider's", got)
	}
	if err := mgr.RefreshProviderCatalog(context.Background(), "bad"); err == nil || !strings.Contains(err.Error(), "no live session") {
		t.Fatalf("the failed provider is still live: %v", err)
	}
}
