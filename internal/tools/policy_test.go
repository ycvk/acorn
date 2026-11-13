package tools_test

import (
	"testing"

	"github.com/ycvk/acorn/internal/tools"
)

// The workstation tools are local tools marked as needing the workstation;
// the web and view tools are not, and no local tool drives a browser.
func TestWorkstationToolsAreMarkedAmongLocalTools(t *testing.T) {
	for name, want := range map[string]bool{"shell": true, "job": true, "follow_up": true, "files": true, "send_file": true, "web_fetch": false, "web_search": false, "show_view": false} {
		if _, ok := tools.ConfiguredLocalSpec(name); !ok {
			t.Fatalf("%s spec missing", name)
		}
		if got := tools.WorkstationTool(name); got != want {
			t.Fatalf("WorkstationTool(%s) = %v, want %v", name, got, want)
		}
	}
	if _, ok := tools.ConfiguredLocalSpec("browser"); ok {
		t.Fatal("browser is not a local tool; the agent drives browsers through the workstation")
	}
}
