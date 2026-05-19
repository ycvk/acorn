package tooling

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
		{"write_scoped", ParallelPolicyWriteScoped, false},
		{"never_parallel", ParallelPolicyNeverParallel, false},
		{"READ_ONLY", ParallelPolicyReadOnly, false},
		{"unknown", "", true},
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
	if len(specs) != 10 {
		t.Fatalf("ConfiguredLocalSpecs len = %d, want 10", len(specs))
	}
	createFile, ok := ConfiguredLocalSpec(cfg, "create_file")
	if !ok {
		t.Fatal("create_file spec missing")
	}
	if createFile.Execution.ParallelPolicy != ParallelPolicyWriteScoped {
		t.Fatalf("create_file parallel = %q, want %q", createFile.Execution.ParallelPolicy, ParallelPolicyWriteScoped)
	}
	if createFile.PlanPolicy != PlanPolicyRequireActivePlan {
		t.Fatalf("create_file plan = %q, want %q", createFile.PlanPolicy, PlanPolicyRequireActivePlan)
	}
	runCommand, ok := ConfiguredLocalSpec(cfg, "run_command")
	if !ok {
		t.Fatal("run_command spec missing")
	}
	if runCommand.Execution.ParallelPolicy != ParallelPolicyNeverParallel {
		t.Fatalf("run_command parallel = %q, want %q", runCommand.Execution.ParallelPolicy, ParallelPolicyNeverParallel)
	}
	rollbackCheckpoint, ok := ConfiguredLocalSpec(cfg, "rollback_workspace_checkpoint")
	if !ok {
		t.Fatal("rollback_workspace_checkpoint spec missing")
	}
	if rollbackCheckpoint.Execution.ParallelPolicy != ParallelPolicyNeverParallel {
		t.Fatalf("rollback_workspace_checkpoint parallel = %q, want %q", rollbackCheckpoint.Execution.ParallelPolicy, ParallelPolicyNeverParallel)
	}
	if rollbackCheckpoint.PlanPolicy != PlanPolicyRequireActivePlan {
		t.Fatalf("rollback_workspace_checkpoint plan = %q, want %q", rollbackCheckpoint.PlanPolicy, PlanPolicyRequireActivePlan)
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
