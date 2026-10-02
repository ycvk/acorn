package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	corestore "github.com/ycvk/acorn/internal/store"
)

type stubRunSearchStore struct{}

func (stubRunSearchStore) SearchRuns(context.Context, string, int) ([]core.RunRecord, error) {
	return nil, nil
}

type stubOperatorStore struct{}

func (stubOperatorStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, nil
}

func (stubOperatorStore) AppendEvent(context.Context, string, string, any) (core.EventRecord, error) {
	return core.EventRecord{}, nil
}

var eagerNativeToolNames = []string{"artifact_list", "artifact_read", "artifact_write", "ask_operator", "search_runs", "worldstate_load", "worldstate_update"}

func testNativeToolDeps(t *testing.T) NativeToolDeps {
	t.Helper()
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), newToolArtifactStore())
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	bridge := fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_1"}
	return NativeToolDeps{
		ArtifactService:   artifactService,
		ArtifactContext:   bridge,
		OperatorStore:     stubOperatorStore{},
		OperatorContext:   bridge,
		RunSearchStore:    stubRunSearchStore{},
		WorldStateUpdater: newStubWorldStateUpdater(),
	}
}

// TestRegisterNativeToolsRegistersEagerLocalTools verifies that
// RegisterNativeTools registers every eager static local tool from
// localToolNames, skips the deferred web tools, and returns specs sorted by name.
func TestRegisterNativeToolsRegistersEagerLocalTools(t *testing.T) {
	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, testNativeToolDeps(t)); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}
	specs := reg.Specs()
	if len(specs) != len(eagerNativeToolNames) {
		t.Fatalf("registered %d specs, want %d", len(specs), len(eagerNativeToolNames))
	}
	for i, spec := range specs {
		if spec.Name != eagerNativeToolNames[i] {
			t.Fatalf("spec[%d] = %q, want %q (sorted)", i, spec.Name, eagerNativeToolNames[i])
		}
	}
	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		if _, ok := reg.Find(name); ok {
			t.Fatalf("deferred tool %q should not be registered", name)
		}
	}
	for name, want := range map[string]core.ToolCategory{
		"search_runs":       core.ToolCategoryInspect,
		"worldstate_load":   core.ToolCategoryInspect,
		"worldstate_update": core.ToolCategoryMemory,
	} {
		spec, _ := reg.Find(name)
		if spec.Kind != core.ToolKindNative || spec.Category != want {
			t.Fatalf("%s contract = %+v, want native/%s", name, spec.ToolContract, want)
		}
		builtin, ok := BuiltinToolSpec(name, "local")
		if !ok || builtin.Category != spec.Category {
			t.Fatalf("%s registry category %s disagrees with built-in contract %+v", name, spec.Category, builtin)
		}
	}
}

// TestRegisterNativeToolsResolveProducesTools verifies that every registered
// eager tool resolves to a concrete instance.
func TestRegisterNativeToolsResolveProducesTools(t *testing.T) {
	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, testNativeToolDeps(t)); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}
	resolved, err := reg.Resolve(context.Background(), core.RunContext{RunID: "r1"}, eagerNativeToolNames)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved) != len(eagerNativeToolNames) {
		t.Fatalf("Resolve returned %d tools, want %d", len(resolved), len(eagerNativeToolNames))
	}
}

// TestRegisterNativeToolsFailsOnMissingDependency verifies that a nil
// dependency fails registration and names the tool and the missing field.
func TestRegisterNativeToolsFailsOnMissingDependency(t *testing.T) {
	cases := []struct {
		field string
		tool  string
		clear func(*NativeToolDeps)
	}{
		{"ArtifactService", "artifact_write", func(d *NativeToolDeps) { d.ArtifactService = nil }},
		{"ArtifactContext", "artifact_write", func(d *NativeToolDeps) { d.ArtifactContext = nil }},
		{"OperatorStore", "ask_operator", func(d *NativeToolDeps) { d.OperatorStore = nil }},
		{"OperatorContext", "ask_operator", func(d *NativeToolDeps) { d.OperatorContext = nil }},
		{"RunSearchStore", "search_runs", func(d *NativeToolDeps) { d.RunSearchStore = nil }},
		{"WorldStateUpdater", "worldstate_update", func(d *NativeToolDeps) { d.WorldStateUpdater = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			deps := testNativeToolDeps(t)
			tc.clear(&deps)
			err := RegisterNativeTools(NewToolRegistry(), deps)
			if err == nil {
				t.Fatalf("RegisterNativeTools without %s: expected error, got nil", tc.field)
			}
			for _, want := range []string{tc.tool, "NativeToolDeps." + tc.field} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestRegisterNativeToolsNilRegistry verifies the nil-registry guard.
func TestRegisterNativeToolsNilRegistry(t *testing.T) {
	if err := RegisterNativeTools(nil, testNativeToolDeps(t)); err == nil {
		t.Fatalf("RegisterNativeTools with nil registry: expected error, got nil")
	}
}

// TestRegistryResolveFailsWhenFactoryReturnsNoTool verifies that a factory
// returning (nil, nil) surfaces as an error from both resolve paths.
func TestRegistryResolveFailsWhenFactoryReturnsNoTool(t *testing.T) {
	reg := NewToolRegistry()
	spec := configuredLocalSpec("search_runs")
	spec.Factory = func(context.Context, core.RunContext) (einotool.BaseTool, error) { return nil, nil }
	if err := reg.Register(spec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx := context.Background()
	if _, err := reg.Resolve(ctx, core.RunContext{}, []string{"search_runs"}); err == nil || !strings.Contains(err.Error(), "returned no tool") {
		t.Fatalf("Resolve error = %v, want returned no tool", err)
	}
	if _, err := reg.ResolveEnabledSpecs(ctx, core.RunContext{}); err == nil || !strings.Contains(err.Error(), "returned no tool") {
		t.Fatalf("ResolveEnabledSpecs error = %v, want returned no tool", err)
	}
}

// Compile-time assertion that the registry returned by NewToolRegistry is
// usable as a core.ToolRegistry.
var _ core.ToolRegistry = (*toolRegistry)(nil)
