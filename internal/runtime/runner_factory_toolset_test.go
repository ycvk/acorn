package runtime

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workingstate"
)

func TestRunnerFactoryBuildServeToolsetOmitsRunOnlyTools(t *testing.T) {
	factory := newRunnerFactoryToolsetTestFactory(t)

	toolset, err := factory.BuildServeToolset(context.Background())
	if err != nil {
		t.Fatalf("BuildServeToolset: %v", err)
	}
	names := toolNamesFromSet(t, toolset.All())

	for _, want := range []string{
		"read_file",
		"create_file",
		"memory_search",
		"memory_read_file",
		"memory_list_files",
		"memory_create_file",
		"memory_replace_span",
		"skill_list",
		"skill_view",
		"skill_create",
	} {
		if !names[want] {
			t.Fatalf("serve toolset missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"delegate_task",
		"update_working_checkpoint",
		"clear_working_checkpoint",
		"hydrate_memory_refs",
		legacyToolName("eval"),
		legacyToolName("curate"),
		"skill_assess",
	} {
		if names[forbidden] {
			t.Fatalf("serve toolset should not include %q", forbidden)
		}
	}
}

func TestRunnerFactoryBuildRunToolsetIncludesRunOnlyTools(t *testing.T) {
	factory := newRunnerFactoryToolsetTestFactory(t)
	subagentExec := NewSubagentExecutor(factory.cfg, factory.store, factory, nil)

	toolset, err := factory.buildRunToolset(context.Background(), "session_test", subagentExec)
	if err != nil {
		t.Fatalf("buildRunToolset: %v", err)
	}
	names := toolNamesFromSet(t, toolset.All())

	for _, want := range []string{
		"delegate_task",
		"update_working_checkpoint",
		"clear_working_checkpoint",
		"memory_search",
		"memory_read_file",
		"memory_list_files",
		"memory_create_file",
		"memory_replace_span",
		"skill_list",
		"skill_view",
		"skill_create",
		"skill_assess",
	} {
		if !names[want] {
			t.Fatalf("run toolset missing %q", want)
		}
	}
	if names["hydrate_memory_refs"] {
		t.Fatal("run toolset should not include hydrate_memory_refs")
	}
}

func newRunnerFactoryToolsetTestFactory(t *testing.T) *RunnerFactory {
	t.Helper()

	store, cfg := newRunnerFactoryMemoryTestContext(t)
	loader := skills.NewLoader(cfg)
	workspace, err := cfg.Workspace()
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}

	return newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		Loader:            loader,
		Workspace:         workspace,
		CheckpointService: workingstate.NewService(store, 4000),
	})
}

func toolNamesFromSet(t *testing.T, tools []einotool.BaseTool) map[string]bool {
	t.Helper()

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		names[info.Name] = true
	}
	return names
}

func legacyToolName(kind string) string {
	return "skill_" + kind
}
