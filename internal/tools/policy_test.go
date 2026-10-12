package tools_test

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func TestConfiguredLocalSpecsDeferWebAndWorkstationTools(t *testing.T) {
	for name, reason := range map[string]string{"web_fetch": "web_access", "web_search": "web_access", "shell": "workstation", "files": "workstation"} {
		spec, ok := tools.ConfiguredLocalSpec(name)
		if !ok {
			t.Fatalf("%s spec missing", name)
		}
		if spec.Loading.Mode != core.ToolLoadingModeDeferred || spec.Loading.Reason != reason {
			t.Fatalf("%s loading = %+v, want deferred/%s", name, spec.Loading, reason)
		}
	}
	if _, ok := tools.ConfiguredLocalSpec("browser"); ok {
		t.Fatal("browser is not a local tool; the agent drives browsers through the workstation")
	}
}
