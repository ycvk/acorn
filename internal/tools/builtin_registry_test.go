package tools

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// TestBuiltinToolNamesSnapshot locks the always-eligible built-in tool list so
// adding/removing a built-in tool is a deliberate, reviewed change.
func TestBuiltinToolNamesSnapshot(t *testing.T) {
	got := BuiltinToolNames()
	want := []string{
		"memory_search",
		"memory_read_file",
		"memory_list_files",
		"memory_create_file",
		"memory_replace_span",
		"remember",
		"skill_list",
		"skill_view",
		"load_tools",
		"ask_operator",
	}
	if len(got) != len(want) {
		t.Fatalf("BuiltinToolNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuiltinToolNames()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestBuiltinToolContractsAreValid is the drift guard: every built-in tool in
// the registry must resolve to a valid contract, so a new built-in cannot be
// added without a complete, correct contract.
func TestBuiltinToolContractsAreValid(t *testing.T) {
	for _, name := range builtinToolOrder {
		contract, ok := BuiltinToolSpec(name, "local")
		if !ok {
			t.Fatalf("BuiltinToolSpec(%q) not found", name)
		}
		if err := contract.Validate(); err != nil {
			t.Fatalf("builtin %q contract invalid: %v", name, err)
		}
	}
}

func TestBuiltinToolSpecUnknownReturnsFalse(t *testing.T) {
	if _, ok := BuiltinToolSpec("not_a_real_tool", "local"); ok {
		t.Fatal("BuiltinToolSpec for unknown tool should return ok=false")
	}
}

// TestWorkingCheckpointToolsAreDeferred documents that deferred built-ins are
// intentionally excluded from the always-eligible list.
func TestWorkingCheckpointToolsAreDeferred(t *testing.T) {
	for _, name := range []string{"update_working_checkpoint", "clear_working_checkpoint"} {
		contract, ok := builtinToolContract(name)
		if !ok {
			t.Fatalf("builtinToolContract(%q) not found", name)
		}
		if contract.Loading.Mode != ToolLoadingModeDeferred {
			t.Fatalf("%q loading = %q, want deferred", name, contract.Loading.Mode)
		}
	}
	for _, name := range BuiltinToolNames() {
		if name == "update_working_checkpoint" || name == "clear_working_checkpoint" {
			t.Fatalf("deferred working-state tool %q must not be in BuiltinToolNames", name)
		}
	}
}

// TestConfiguredLocalSpecRoundtrip guards the static local toolset: the list and
// the by-name lookup derive from one source, so every listed spec is valid and
// resolvable, and unknown names are rejected.
func TestConfiguredLocalSpecRoundtrip(t *testing.T) {
	cfg := &config.Config{}
	specs := ConfiguredLocalSpecs(cfg)
	if len(specs) == 0 {
		t.Fatal("ConfiguredLocalSpecs returned none")
	}
	for _, spec := range specs {
		if err := spec.ToolContract.Validate(); err != nil {
			t.Fatalf("local spec %q invalid: %v", spec.Name, err)
		}
		got, ok := ConfiguredLocalSpec(cfg, spec.Name)
		if !ok {
			t.Fatalf("ConfiguredLocalSpec(%q) returned ok=false but it is in ConfiguredLocalSpecs", spec.Name)
		}
		if got.Name != spec.Name {
			t.Fatalf("ConfiguredLocalSpec(%q).Name = %q", spec.Name, got.Name)
		}
	}
	if _, ok := ConfiguredLocalSpec(cfg, "definitely_not_a_local_tool"); ok {
		t.Fatal("ConfiguredLocalSpec for unknown tool should return ok=false")
	}
}
