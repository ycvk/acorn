package wire

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/tools"
)

// TestRunCatalogContainsEveryBuiltinTool guards against registry factories
// that silently drop a built-in tool when the container forgets to inject a
// dependency it needs.
func TestRunCatalogContainsEveryBuiltinTool(t *testing.T) {
	cfg := writeApprovalTestConfig(t, "http://127.0.0.1:1")
	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer func() { _ = c.Close() }()
	thread, err := c.Threads().CreateThread(ctx, "catalog")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if err := c.store.CreateRun(ctx, core.RunCreateParams{RunID: "run_catalog", SessionID: thread.ID, Input: "probe"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	active, err := c.runnerFactory.New(ctx, runtime.RunnerBuildRequest{SessionID: thread.ID, RunID: "run_catalog", Input: "probe"})
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	defer func() { _ = active.Close() }()
	present := map[string]bool{}
	for _, spec := range active.ToolCatalog.EnabledSpecs() {
		present[spec.Name] = true
	}
	for _, name := range tools.BuiltinToolNames() {
		if !present[name] {
			t.Errorf("built-in tool %s is missing from the run catalog", name)
		}
	}
}
