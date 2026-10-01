package tools_test

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func TestConfiguredLocalSpecsDeferWebTools(t *testing.T) {
	cfg := defaultToolingTestConfig()
	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		spec, ok := tools.ConfiguredLocalSpec(cfg, name)
		if !ok {
			t.Fatalf("%s spec missing", name)
		}
		if spec.Loading.Mode != core.ToolLoadingModeDeferred || spec.Loading.Reason != "web_access" {
			t.Fatalf("%s loading = %+v, want deferred/web_access", name, spec.Loading)
		}
	}
}

func defaultToolingTestConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Workspace:  config.WorkspaceToolConfig{RootDir: "."},
			Mutation:   config.MutationToolConfig{RootDir: "."},
			RunCommand: config.RunCommandToolConfig{WorkDir: "."},
		},
	}
}
