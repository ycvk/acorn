package tools_test

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func TestConfiguredLocalSpecsLoadWebToolsAndDeferWorkstationTools(t *testing.T) {
	for _, name := range []string{"web_fetch", "web_search"} {
		spec, ok := tools.ConfiguredLocalSpec(name)
		if !ok {
			t.Fatalf("%s spec missing", name)
		}
		if spec.Loading.Mode != core.ToolLoadingModeEager {
			t.Fatalf("%s loading = %+v, want eager", name, spec.Loading)
		}
	}
	for _, name := range []string{"shell", "files"} {
		spec, ok := tools.ConfiguredLocalSpec(name)
		if !ok {
			t.Fatalf("%s spec missing", name)
		}
		if spec.Loading.Mode != core.ToolLoadingModeDeferred || spec.Loading.Reason != "workstation" {
			t.Fatalf("%s loading = %+v, want deferred/workstation", name, spec.Loading)
		}
	}
	if _, ok := tools.ConfiguredLocalSpec("browser"); ok {
		t.Fatal("browser is not a local tool; the agent drives browsers through the workstation")
	}
}
