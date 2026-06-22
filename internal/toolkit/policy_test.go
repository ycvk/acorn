package toolkit

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestParallelPolicyStringParsing(t *testing.T) {
	tests := []struct {
		input   string
		want    ParallelPolicy
		wantErr bool
	}{
		{"read_only", ParallelPolicyReadOnly, false},
		{"readonly", ParallelPolicyReadOnly, false},
		{"serial", ParallelPolicySerial, false},
		{"SERIAL", ParallelPolicySerial, false},
		{"READ_ONLY", ParallelPolicyReadOnly, false},
		{"unknown", "", true},
		{"write_scoped", "", true},
		{"never_parallel", "", true},
	}
	for _, tt := range tests {
		got, err := ParseParallelPolicy(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseParallelPolicy(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseParallelPolicy(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseParallelPolicy(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestConfiguredLocalSpecsCarryCanonicalPolicies(t *testing.T) {
	cfg := defaultToolingTestConfig()
	specs := ConfiguredLocalSpecs(cfg)
	if len(specs) != 20 {
		t.Fatalf("ConfiguredLocalSpecs len = %d, want 20", len(specs))
	}
	createFile, ok := ConfiguredLocalSpec(cfg, "create_file")
	if !ok {
		t.Fatal("create_file spec missing")
	}
	if createFile.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("create_file parallel = %q, want %q", createFile.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	runCommand, ok := ConfiguredLocalSpec(cfg, "run_command")
	if !ok {
		t.Fatal("run_command spec missing")
	}
	if runCommand.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("run_command parallel = %q, want %q", runCommand.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	runVerification, ok := ConfiguredLocalSpec(cfg, "run_verification")
	if !ok {
		t.Fatal("run_verification spec missing")
	}
	if runVerification.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("run_verification parallel = %q, want %q", runVerification.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	multiEdit, ok := ConfiguredLocalSpec(cfg, "multi_edit")
	if !ok {
		t.Fatal("multi_edit spec missing")
	}
	if multiEdit.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("multi_edit parallel = %q, want %q", multiEdit.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	gitSummary, ok := ConfiguredLocalSpec(cfg, "git_summary")
	if !ok {
		t.Fatal("git_summary spec missing")
	}
	if gitSummary.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("git_summary parallel = %q, want %q", gitSummary.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	rollbackCheckpoint, ok := ConfiguredLocalSpec(cfg, "rollback_workspace_checkpoint")
	if !ok {
		t.Fatal("rollback_workspace_checkpoint spec missing")
	}
	if rollbackCheckpoint.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("rollback_workspace_checkpoint parallel = %q, want %q", rollbackCheckpoint.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	artifactWrite, ok := ConfiguredLocalSpec(cfg, "artifact_write")
	if !ok {
		t.Fatal("artifact_write spec missing")
	}
	if artifactWrite.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("artifact_write parallel = %q, want %q", artifactWrite.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	artifactRead, ok := ConfiguredLocalSpec(cfg, "artifact_read")
	if !ok {
		t.Fatal("artifact_read spec missing")
	}
	if artifactRead.Execution.ParallelPolicy != ParallelPolicyReadOnly {
		t.Fatalf("artifact_read parallel = %q, want %q", artifactRead.Execution.ParallelPolicy, ParallelPolicyReadOnly)
	}
	askOperator, ok := ConfiguredLocalSpec(cfg, "ask_operator")
	if !ok {
		t.Fatal("ask_operator spec missing")
	}
	if askOperator.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("ask_operator parallel = %q, want %q", askOperator.Execution.ParallelPolicy, ParallelPolicySerial)
	}
	webFetch, ok := ConfiguredLocalSpec(cfg, "web_fetch")
	if !ok {
		t.Fatal("web_fetch spec missing")
	}
	if webFetch.Loading.Mode != ToolLoadingModeDeferred || webFetch.Loading.Reason != "web_access" {
		t.Fatalf("web_fetch loading = %+v, want deferred/web_access", webFetch.Loading)
	}
	if webFetch.Execution.ParallelPolicy != ParallelPolicyReadOnly {
		t.Fatalf("web_fetch parallel = %q, want %q", webFetch.Execution.ParallelPolicy, ParallelPolicyReadOnly)
	}
	webSearch, ok := ConfiguredLocalSpec(cfg, "web_search")
	if !ok {
		t.Fatal("web_search spec missing")
	}
	if webSearch.Loading.Mode != ToolLoadingModeDeferred || webSearch.Loading.Reason != "web_access" {
		t.Fatalf("web_search loading = %+v, want deferred/web_access", webSearch.Loading)
	}
	if webSearch.Execution.ParallelPolicy != ParallelPolicyReadOnly {
		t.Fatalf("web_search parallel = %q, want %q", webSearch.Execution.ParallelPolicy, ParallelPolicyReadOnly)
	}
	browser, ok := ConfiguredLocalSpec(cfg, "browser")
	if !ok {
		t.Fatal("browser spec missing")
	}
	if browser.Loading.Mode != ToolLoadingModeDeferred || browser.Loading.Reason != "web_access" {
		t.Fatalf("browser loading = %+v, want deferred/web_access", browser.Loading)
	}
	if browser.Execution.ParallelPolicy != ParallelPolicySerial {
		t.Fatalf("browser parallel = %q, want %q", browser.Execution.ParallelPolicy, ParallelPolicySerial)
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
