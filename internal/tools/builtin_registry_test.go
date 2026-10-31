package tools

import (
	"testing"
)

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
