package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	corestore "github.com/ycvk/acorn/internal/store"
)

type stubRunSearchStore struct{}

func (stubRunSearchStore) SearchRuns(context.Context, string, int) ([]core.RunRecord, error) {
	return nil, nil
}

// TestRegisterNativeToolsRegistersEagerLocalTools verifies that
// RegisterNativeTools registers every eager static local tool from
// localToolNames, skips the deferred web tools, and returns specs sorted by name.
func TestRegisterNativeToolsRegistersEagerLocalTools(t *testing.T) {
	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, CatalogConfig{}); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}
	specs := reg.Specs()
	want := []string{"artifact_list", "artifact_read", "artifact_write", "ask_operator", "search_runs", "worldstate_load", "worldstate_update"}
	if len(specs) != len(want) {
		t.Fatalf("registered %d specs, want %d", len(specs), len(want))
	}
	for i, spec := range specs {
		if spec.Name != want[i] {
			t.Fatalf("spec[%d] = %q, want %q (sorted)", i, spec.Name, want[i])
		}
	}
	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		if _, ok := reg.Find(name); ok {
			t.Fatalf("deferred tool %q should not be registered", name)
		}
	}
	searchRuns, _ := reg.Find("search_runs")
	if searchRuns.Kind != core.ToolKindNative || searchRuns.Category != core.ToolCategoryInspect {
		t.Fatalf("search_runs contract = %+v", searchRuns.ToolContract)
	}
}

// TestRegisterNativeToolsResolveProducesTools verifies that Resolve invokes the
// per-tool factories when their dependencies are present.
func TestRegisterNativeToolsResolveProducesTools(t *testing.T) {
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), newToolArtifactStore())
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, CatalogConfig{
		ArtifactService: artifactService,
		ArtifactContext: fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_1"},
		RunSearchStore:  stubRunSearchStore{},
	}); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}
	names := []string{"artifact_write", "artifact_read", "artifact_list", "search_runs"}
	resolved, err := reg.Resolve(context.Background(), core.RunContext{RunID: "r1"}, names)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved) != len(names) {
		t.Fatalf("Resolve returned %d tools, want %d", len(resolved), len(names))
	}
}

// TestRegisterNativeToolsNilRegistry verifies the nil-registry guard.
func TestRegisterNativeToolsNilRegistry(t *testing.T) {
	if err := RegisterNativeTools(nil, CatalogConfig{}); err == nil {
		t.Fatalf("RegisterNativeTools with nil registry: expected error, got nil")
	}
}

// Compile-time assertion that the registry returned by NewToolRegistry is
// usable as a core.ToolRegistry.
var _ core.ToolRegistry = (*toolRegistry)(nil)
