package tools_test

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func TestParallelPolicyStringParsing(t *testing.T) {
	tests := []struct {
		input   string
		want    core.ParallelPolicy
		wantErr bool
	}{
		{"read_only", core.ParallelPolicyReadOnly, false},
		{"readonly", core.ParallelPolicyReadOnly, false},
		{"serial", core.ParallelPolicySerial, false},
		{"SERIAL", core.ParallelPolicySerial, false},
		{"READ_ONLY", core.ParallelPolicyReadOnly, false},
		{"unknown", "", true},
		{"write_scoped", "", true},
		{"never_parallel", "", true},
	}
	for _, tt := range tests {
		got, err := core.ParseParallelPolicy(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("core.ParseParallelPolicy(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("core.ParseParallelPolicy(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("core.ParseParallelPolicy(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestConfiguredLocalSpecsCarryCanonicalPolicies(t *testing.T) {
	cfg := defaultToolingTestConfig()
	specs := tools.ConfiguredLocalSpecs(cfg)
	if len(specs) != 20 {
		t.Fatalf("ConfiguredLocalSpecs len = %d, want 20", len(specs))
	}
	createFile, ok := tools.ConfiguredLocalSpec(cfg, "create_file")
	if !ok {
		t.Fatal("create_file spec missing")
	}
	if createFile.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("create_file parallel = %q, want %q", createFile.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	runCommand, ok := tools.ConfiguredLocalSpec(cfg, "run_command")
	if !ok {
		t.Fatal("run_command spec missing")
	}
	if runCommand.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("run_command parallel = %q, want %q", runCommand.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	runVerification, ok := tools.ConfiguredLocalSpec(cfg, "run_verification")
	if !ok {
		t.Fatal("run_verification spec missing")
	}
	if runVerification.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("run_verification parallel = %q, want %q", runVerification.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	multiEdit, ok := tools.ConfiguredLocalSpec(cfg, "multi_edit")
	if !ok {
		t.Fatal("multi_edit spec missing")
	}
	if multiEdit.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("multi_edit parallel = %q, want %q", multiEdit.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	gitSummary, ok := tools.ConfiguredLocalSpec(cfg, "git_summary")
	if !ok {
		t.Fatal("git_summary spec missing")
	}
	if gitSummary.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("git_summary parallel = %q, want %q", gitSummary.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	rollbackCheckpoint, ok := tools.ConfiguredLocalSpec(cfg, "rollback_workspace_checkpoint")
	if !ok {
		t.Fatal("rollback_workspace_checkpoint spec missing")
	}
	if rollbackCheckpoint.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("rollback_workspace_checkpoint parallel = %q, want %q", rollbackCheckpoint.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	artifactWrite, ok := tools.ConfiguredLocalSpec(cfg, "artifact_write")
	if !ok {
		t.Fatal("artifact_write spec missing")
	}
	if artifactWrite.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("artifact_write parallel = %q, want %q", artifactWrite.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	artifactRead, ok := tools.ConfiguredLocalSpec(cfg, "artifact_read")
	if !ok {
		t.Fatal("artifact_read spec missing")
	}
	if artifactRead.Execution.ParallelPolicy != core.ParallelPolicyReadOnly {
		t.Fatalf("artifact_read parallel = %q, want %q", artifactRead.Execution.ParallelPolicy, core.ParallelPolicyReadOnly)
	}
	askOperator, ok := tools.ConfiguredLocalSpec(cfg, "ask_operator")
	if !ok {
		t.Fatal("ask_operator spec missing")
	}
	if askOperator.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("ask_operator parallel = %q, want %q", askOperator.Execution.ParallelPolicy, core.ParallelPolicySerial)
	}
	webFetch, ok := tools.ConfiguredLocalSpec(cfg, "web_fetch")
	if !ok {
		t.Fatal("web_fetch spec missing")
	}
	if webFetch.Loading.Mode != core.ToolLoadingModeDeferred || webFetch.Loading.Reason != "web_access" {
		t.Fatalf("web_fetch loading = %+v, want deferred/web_access", webFetch.Loading)
	}
	if webFetch.Execution.ParallelPolicy != core.ParallelPolicyReadOnly {
		t.Fatalf("web_fetch parallel = %q, want %q", webFetch.Execution.ParallelPolicy, core.ParallelPolicyReadOnly)
	}
	webSearch, ok := tools.ConfiguredLocalSpec(cfg, "web_search")
	if !ok {
		t.Fatal("web_search spec missing")
	}
	if webSearch.Loading.Mode != core.ToolLoadingModeDeferred || webSearch.Loading.Reason != "web_access" {
		t.Fatalf("web_search loading = %+v, want deferred/web_access", webSearch.Loading)
	}
	if webSearch.Execution.ParallelPolicy != core.ParallelPolicyReadOnly {
		t.Fatalf("web_search parallel = %q, want %q", webSearch.Execution.ParallelPolicy, core.ParallelPolicyReadOnly)
	}
	browser, ok := tools.ConfiguredLocalSpec(cfg, "browser")
	if !ok {
		t.Fatal("browser spec missing")
	}
	if browser.Loading.Mode != core.ToolLoadingModeDeferred || browser.Loading.Reason != "web_access" {
		t.Fatalf("browser loading = %+v, want deferred/web_access", browser.Loading)
	}
	if browser.Execution.ParallelPolicy != core.ParallelPolicySerial {
		t.Fatalf("browser parallel = %q, want %q", browser.Execution.ParallelPolicy, core.ParallelPolicySerial)
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
