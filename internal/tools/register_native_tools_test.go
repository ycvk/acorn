package tools

import (
	"context"
	"path/filepath"
	"testing"

	corestore "github.com/ycvk/acorn/internal/store"

	"github.com/ycvk/acorn/internal/core"
)

// TestRegisterNativeToolsRegistersAllLocalTools verifies that
// RegisterNativeTools registers every static local tool declared by
// localToolDefs and that Specs() returns them sorted by name. This is an
// internal test (package tools) so it can reuse the workspace/artifact test
// fixtures in tools_test.go.
func TestRegisterNativeToolsRegistersAllLocalTools(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), newToolArtifactStore())
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	ws := testWorkspace(t, root)
	cfg := CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   true,
		RunCommandEnabled: true,
		ArtifactService:   artifactService,
		ArtifactContext:   fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_1"},
	}

	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, cfg); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}

	specs := reg.Specs()
	// localToolDefs declares 21 static local tools; 3 are deferred-loaded
	// (web_fetch, web_search, browser) and excluded from the registry because
	// they depend on per-run services. The remaining 18 eager tools are registered.
	wantCount := 18
	if got := len(specs); got != wantCount {
		t.Fatalf("registered spec count = %d, want %d", got, wantCount)
	}

	// Deferred tools must NOT be in the registry.
	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		if _, ok := reg.Find(name); ok {
			t.Fatalf("deferred tool %q should not be registered", name)
		}
	}

	// Specs must be sorted by name.
	for i := 1; i < len(specs); i++ {
		if specs[i-1].Name >= specs[i].Name {
			t.Fatalf("Specs not sorted by name: %q >= %q at index %d", specs[i-1].Name, specs[i].Name, i)
		}
	}

	// Spot-check a known tool's contract conversion.
	gitSummary, ok := reg.Find("git_summary")
	if !ok {
		t.Fatalf("Find git_summary: not found")
	}
	if got, want := gitSummary.Kind, core.ToolKindNative; got != want {
		t.Fatalf("git_summary.Kind = %q, want %q", got, want)
	}
	if got, want := gitSummary.Category, core.ToolCategoryInspect; got != want {
		t.Fatalf("git_summary.Category = %q, want %q", got, want)
	}
	if got, want := gitSummary.Execution.ParallelPolicy, core.ParallelPolicySerial; got != want {
		t.Fatalf("git_summary.ParallelPolicy = %q, want %q", got, want)
	}
}

// TestRegisterNativeToolsResolveProducesTools verifies that Resolve invokes the
// per-tool factories and returns concrete einotool.BaseTool instances for the
// workspace-backed tools.
func TestRegisterNativeToolsResolveProducesTools(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	cfg := CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   true,
		RunCommandEnabled: true,
	}

	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, cfg); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}

	names := []string{"read_file", "list_files", "search_text", "create_file", "run_command"}
	resolved, err := reg.Resolve(context.Background(), core.RunContext{RunID: "r1"}, names)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := len(resolved), len(names); got != want {
		t.Fatalf("Resolve returned %d tools, want %d", got, want)
	}
	for i, tool := range resolved {
		if tool == nil {
			t.Fatalf("Resolve[%d] is nil", i)
		}
	}
}

// TestRegisterNativeToolsDisabledMutationTools verifies that when
// MutationEnabled is false, the mutation tools are registered with a Disabled
// health state (matching configuredLocalSpec) and thus excluded from
// EnabledSpecs.
func TestRegisterNativeToolsDisabledMutationTools(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	cfg := CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   false,
		RunCommandEnabled: false,
	}

	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, cfg); err != nil {
		t.Fatalf("RegisterNativeTools: %v", err)
	}

	createFileSpec, ok := reg.Find("create_file")
	if !ok {
		t.Fatalf("Find create_file: not found")
	}
	if createFileSpec.Enabled() {
		t.Fatalf("create_file should be disabled when MutationEnabled=false")
	}
	if got, want := createFileSpec.Health.State, core.HealthStateDisabled; got != want {
		t.Fatalf("create_file.Health.State = %q, want %q", got, want)
	}

	runCommandSpec, ok := reg.Find("run_command")
	if !ok {
		t.Fatalf("Find run_command: not found")
	}
	if runCommandSpec.Enabled() {
		t.Fatalf("run_command should be disabled when RunCommandEnabled=false")
	}

	// read_file is always-on baseline and should remain enabled even without
	// mutation/run_command.
	readFileSpec, ok := reg.Find("read_file")
	if !ok {
		t.Fatalf("Find read_file: not found")
	}
	if !readFileSpec.Enabled() {
		t.Fatalf("read_file should be enabled regardless of mutation/run_command flags")
	}
}

// TestRegisterNativeToolsNilRegistry verifies the nil-registry guard.
func TestRegisterNativeToolsNilRegistry(t *testing.T) {
	if err := RegisterNativeTools(nil, CatalogConfig{}); err == nil {
		t.Fatalf("RegisterNativeTools with nil registry: expected error, got nil")
	}
}

// TestRegisterNativeToolsEmptyConfigStillRegisters verifies that a zero
// CatalogConfig registers eager-loaded tools (factories return nil tool for
// absent services, but the specs/contracts are still present). Deferred-loaded
// tools (web/browser) are not registered.
func TestRegisterNativeToolsEmptyConfigStillRegisters(t *testing.T) {
	reg := NewToolRegistry()
	if err := RegisterNativeTools(reg, CatalogConfig{}); err != nil {
		t.Fatalf("RegisterNativeTools with empty config: %v", err)
	}
	specs := reg.Specs()
	if got := len(specs); got == 0 {
		t.Fatalf("expected specs registered even with empty config, got 0")
	}
	// read_file is present but its factory returns nil (no workspace); Resolve
	// should skip it without error.
	resolved, err := reg.Resolve(context.Background(), core.RunContext{}, []string{"read_file"})
	if err != nil {
		t.Fatalf("Resolve read_file with no workspace: %v", err)
	}
	if got := len(resolved); got != 0 {
		t.Fatalf("Resolve read_file with no workspace returned %d tools, want 0 (skipped)", got)
	}
}

// Compile-time assertion that the registry returned by NewToolRegistry is
// usable as a core.ToolRegistry.
var _ core.ToolRegistry = (*toolRegistry)(nil)
