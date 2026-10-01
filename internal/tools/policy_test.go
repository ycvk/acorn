package tools_test

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func TestConfiguredLocalSpecsDeferWebTools(t *testing.T) {
	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		spec, ok := tools.ConfiguredLocalSpec(name)
		if !ok {
			t.Fatalf("%s spec missing", name)
		}
		if spec.Loading.Mode != core.ToolLoadingModeDeferred || spec.Loading.Reason != "web_access" {
			t.Fatalf("%s loading = %+v, want deferred/web_access", name, spec.Loading)
		}
	}
}
