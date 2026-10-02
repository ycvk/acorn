package tools

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// TestBuiltinToolNamesSnapshot locks the always-eligible built-in tool list so
// adding/removing a built-in tool is a deliberate, reviewed change.
func TestBuiltinToolNamesSnapshot(t *testing.T) {
	got := BuiltinToolNames()
	want := []string{
		"skill_list",
		"skill_view",
		"skill_create",
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

// TestConfiguredLocalSpecRoundtrip guards the static local toolset: the list and
// the by-name lookup derive from one source, so every listed spec is valid and
// resolvable, and unknown names are rejected.
func TestConfiguredLocalSpecRoundtrip(t *testing.T) {
	specs := ConfiguredLocalSpecs()
	if len(specs) == 0 {
		t.Fatal("ConfiguredLocalSpecs returned none")
	}
	for _, spec := range specs {
		if err := spec.ToolContract.Validate(); err != nil {
			t.Fatalf("local spec %q invalid: %v", spec.Name, err)
		}
		got, ok := ConfiguredLocalSpec(spec.Name)
		if !ok {
			t.Fatalf("ConfiguredLocalSpec(%q) returned ok=false but it is in ConfiguredLocalSpecs", spec.Name)
		}
		if got.Name != spec.Name {
			t.Fatalf("ConfiguredLocalSpec(%q).Name = %q", spec.Name, got.Name)
		}
	}
	if _, ok := ConfiguredLocalSpec("definitely_not_a_local_tool"); ok {
		t.Fatal("ConfiguredLocalSpec for unknown tool should return ok=false")
	}
}

// BuiltinToolNames treats every built-in as always eligible, which only holds
// while no built-in is deferred.
func TestBuiltinToolsAreEager(t *testing.T) {
	for _, name := range builtinToolOrder {
		contract, ok := builtinToolContract(name)
		if !ok {
			t.Fatalf("builtinToolContract(%q) not found", name)
		}
		if contract.Loading.Mode != core.ToolLoadingModeEager {
			t.Fatalf("built-in %q loading = %q, want eager", name, contract.Loading.Mode)
		}
	}
}
