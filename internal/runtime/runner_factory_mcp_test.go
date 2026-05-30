package runtime

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
)

func mcpRunnerBuildRequest(sessionID, runID string) RunnerBuildRequest {
	return RunnerBuildRequest{
		SessionID:         sessionID,
		RunID:             runID,
		Input:             "test input",
		OrchestrationMode: events.ModeSingleAgent,
	}
}

func TestRunnerFactoryReusesMCPManagerForSamePreparedConfig(t *testing.T) {
	binary := buildMCPFixtureServer(t)
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "reused-provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	t.Cleanup(func() { _ = factory.Close() })

	ctx := context.Background()

	first, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_reuse_1", "run_reuse_1"))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	_ = first.Close()

	second, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_reuse_2", "run_reuse_2"))
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	_ = second.Close()

	factory.mu.Lock()
	cached := factory.cachedManager
	factory.mu.Unlock()

	if cached == nil {
		t.Fatal("expected cached manager after two runs with same config")
	}
	statuses := cached.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("cached manager status count = %d, want %d", got, want)
	}
}

func TestRunnerFactorySurvivesBaseConfigHotReload(t *testing.T) {
	binary := buildMCPFixtureServer(t)
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "provider-a",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	t.Cleanup(func() { _ = factory.Close() })

	ctx := context.Background()

	first, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_survive_1", "run_survive_1"))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	_ = first.Close()

	factory.mu.Lock()
	firstManager := factory.cachedManager
	factory.mu.Unlock()

	// Change the MCP provider config — the manager instance should survive
	// because base config changes no longer force a whole-manager rebuild.
	// The reconciliation happens on the live manager instead.
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "provider-b",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	second, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_survive_2", "run_survive_2"))
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	_ = second.Close()

	factory.mu.Lock()
	secondManager := factory.cachedManager
	factory.mu.Unlock()

	if firstManager != secondManager {
		t.Fatal("expected the same manager instance after base config change — manager should not be rebuilt")
	}
}

func TestRunnerFactorySessionOverlayTriggersReconciliation(t *testing.T) {
	binary := buildMCPFixtureServer(t)
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "overlay-provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	t.Cleanup(func() { _ = factory.Close() })

	ctx := context.Background()

	// First run establishes a session overlay.
	first, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_no_overlay", "run_no_overlay"))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	_ = first.Close()

	factory.mu.Lock()
	firstManager := factory.cachedManager
	firstOverlay := factory.lastSessionOverlay
	factory.mu.Unlock()

	if firstManager == nil {
		t.Fatal("expected cached manager after first run")
	}
	if firstOverlay != "thread_run_no_overlay" {
		t.Fatalf("expected first session overlay to be recorded, got %q", firstOverlay)
	}

	// Second run with a different session ID — the session overlay changes,
	// so the factory should reconcile the provider set on the live manager.
	// The manager instance should remain the same.
	second, err := factory.New(ctx, mcpRunnerBuildRequest("sess_overlay_test", "run_with_overlay"))
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	_ = second.Close()

	factory.mu.Lock()
	secondManager := factory.cachedManager
	secondOverlay := factory.lastSessionOverlay
	factory.mu.Unlock()

	if firstManager != secondManager {
		t.Fatal("expected the same manager instance when session overlay changes — reconciliation should be in-place")
	}
	if secondOverlay != "sess_overlay_test" {
		t.Fatalf("expected session overlay to be updated, got %q", secondOverlay)
	}

	// Third run with the same session ID — no reconciliation needed.
	third, err := factory.New(ctx, mcpRunnerBuildRequest("sess_overlay_test", "run_same_overlay"))
	if err != nil {
		t.Fatalf("third New: %v", err)
	}
	_ = third.Close()

	factory.mu.Lock()
	thirdManager := factory.cachedManager
	thirdOverlay := factory.lastSessionOverlay
	factory.mu.Unlock()

	if firstManager != thirdManager {
		t.Fatal("expected the same manager instance for same overlay")
	}
	if thirdOverlay != "sess_overlay_test" {
		t.Fatalf("expected session overlay to remain %q, got %q", "sess_overlay_test", thirdOverlay)
	}
}

func TestRunnerFactoryCloseReleasesManager(t *testing.T) {
	binary := buildMCPFixtureServer(t)
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "close-provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})

	ctx := context.Background()
	active, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_close_test", "run_close_test"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = active.Close()

	factory.mu.Lock()
	cachedBefore := factory.cachedManager
	factory.mu.Unlock()

	if cachedBefore == nil {
		t.Fatal("expected cached manager before Close()")
	}

	if err := factory.Close(); err != nil {
		t.Fatalf("factory Close: %v", err)
	}

	factory.mu.Lock()
	cachedAfter := factory.cachedManager
	factory.mu.Unlock()

	if cachedAfter != nil {
		t.Fatal("expected cached manager to be nil after Close()")
	}
}

func TestRunnerFactoryEventCallbackDoesNotImportRuntime(t *testing.T) {
	// Verify that internal/providers/mcp does not import internal/runtime.
	// This is a compile-time check: if the import existed, the code would not compile
	// due to a cycle. We verify by checking the import list of the mcp package.
	//
	// This test validates the "no package cycle" acceptance criteria.
	//
	// The real proof is that `go build ./...` succeeds, but we also check the
	// source directly to make the invariant explicit.
	output, err := exec.Command("go", "list", "-f", "{{.Imports}}", "github.com/ycvk/acorn/internal/providers/mcp").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, string(output))
	}
	imports := string(output)
	// Check for the exact runtime package path, not sub-packages like runtime/stream.
	importList := strings.Trim(imports, "[]")
	for _, pkg := range strings.Fields(importList) {
		if pkg == "github.com/ycvk/acorn/internal/runtime" {
			t.Fatalf("internal/providers/mcp must NOT import internal/runtime; imports: %s", imports)
		}
	}
}

func TestRunnerFactoryActiveRunIDClearedOnClose(t *testing.T) {
	binary := buildMCPFixtureServer(t)
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "run-id-provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	t.Cleanup(func() { _ = factory.Close() })

	ctx := context.Background()

	// Build first run
	first, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_first", "run_first"))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}

	firstActiveID, _ := factory.currentRunID.Load().(string)

	if got, want := firstActiveID, "run_first"; got != want {
		t.Fatalf("active run ID during first run = %q, want %q", got, want)
	}

	// Close the first runner — should clear the active run ID
	_ = first.Close()

	clearedID, _ := factory.currentRunID.Load().(string)

	if clearedID != "" {
		t.Fatalf("active run ID after Close = %q, want empty", clearedID)
	}

	// Build second run — should use a different run ID
	second, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_second", "run_second"))
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	defer second.Close()

	secondActiveID, _ := factory.currentRunID.Load().(string)

	if got, want := secondActiveID, "run_second"; got != want {
		t.Fatalf("active run ID during second run = %q, want %q", got, want)
	}
}

func TestRunnerFactoryClearsActiveRunIDOnClose(t *testing.T) {
	binary := buildMCPFixtureServer(t)
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "event-provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	t.Cleanup(func() { _ = factory.Close() })

	ctx := context.Background()
	active, err := factory.New(ctx, mcpRunnerBuildRequest("thread_run_reconnect_event", "run_mcp_event"))
	if err != nil {
		t.Fatalf("RunnerFactory.New: %v", err)
	}
	factory.mu.Lock()
	mgr := factory.cachedManager
	factory.mu.Unlock()

	if mgr == nil {
		t.Fatal("expected cached manager")
	}

	_ = active.Close()

	// After close, no stale run ID should be present.
	clearedID, _ := factory.currentRunID.Load().(string)

	if clearedID != "" {
		t.Fatalf("active run ID after Close = %q, want empty (no stale ID)", clearedID)
	}
}
