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
	if len(specs) != 20 {
		t.Fatalf("ConfiguredLocalSpecs len = %d, want 20", len(specs))
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
	runVerification, ok := ConfiguredLocalSpec(cfg, "run_verification")
	if !ok {
		t.Fatal("run_verification spec missing")
	}
	if runVerification.Execution.ParallelPolicy != ParallelPolicyNeverParallel || runVerification.PlanPolicy != PlanPolicyRequireActivePlan {
		t.Fatalf("run_verification policy = parallel:%q plan:%q", runVerification.Execution.ParallelPolicy, runVerification.PlanPolicy)
	}
	multiEdit, ok := ConfiguredLocalSpec(cfg, "multi_edit")
	if !ok {
		t.Fatal("multi_edit spec missing")
	}
	if multiEdit.Execution.ParallelPolicy != ParallelPolicyNeverParallel || multiEdit.PlanPolicy != PlanPolicyRequireActivePlan {
		t.Fatalf("multi_edit policy = parallel:%q plan:%q", multiEdit.Execution.ParallelPolicy, multiEdit.PlanPolicy)
	}
	gitSummary, ok := ConfiguredLocalSpec(cfg, "git_summary")
	if !ok {
		t.Fatal("git_summary spec missing")
	}
	if gitSummary.PlanPolicy != PlanPolicyNone || gitSummary.Execution.ParallelPolicy != ParallelPolicyNeverParallel {
		t.Fatalf("git_summary policy = parallel:%q plan:%q", gitSummary.Execution.ParallelPolicy, gitSummary.PlanPolicy)
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
	artifactWrite, ok := ConfiguredLocalSpec(cfg, "artifact_write")
	if !ok {
		t.Fatal("artifact_write spec missing")
	}
	if artifactWrite.ResourceScope != ResourceScopeArtifact || artifactWrite.Execution.ParallelPolicy != ParallelPolicyNeverParallel {
		t.Fatalf("artifact_write policy = scope:%q parallel:%q", artifactWrite.ResourceScope, artifactWrite.Execution.ParallelPolicy)
	}
	if artifactWrite.PlanPolicy != PlanPolicyNone {
		t.Fatalf("artifact_write plan = %q, want %q", artifactWrite.PlanPolicy, PlanPolicyNone)
	}
	artifactRead, ok := ConfiguredLocalSpec(cfg, "artifact_read")
	if !ok {
		t.Fatal("artifact_read spec missing")
	}
	if artifactRead.ResourceScope != ResourceScopeArtifact || artifactRead.Execution.ParallelPolicy != ParallelPolicyReadOnly {
		t.Fatalf("artifact_read policy = scope:%q parallel:%q", artifactRead.ResourceScope, artifactRead.Execution.ParallelPolicy)
	}
	askOperator, ok := ConfiguredLocalSpec(cfg, "ask_operator")
	if !ok {
		t.Fatal("ask_operator spec missing")
	}
	if askOperator.ResourceScope != ResourceScopeOperator || askOperator.Execution.ParallelPolicy != ParallelPolicyNeverParallel {
		t.Fatalf("ask_operator policy = scope:%q parallel:%q", askOperator.ResourceScope, askOperator.Execution.ParallelPolicy)
	}
	if askOperator.PlanPolicy != PlanPolicyNone {
		t.Fatalf("ask_operator plan = %q, want %q", askOperator.PlanPolicy, PlanPolicyNone)
	}
	webFetch, ok := ConfiguredLocalSpec(cfg, "web_fetch")
	if !ok {
		t.Fatal("web_fetch spec missing")
	}
	if webFetch.ResourceScope != ResourceScopeWeb || webFetch.Loading.Mode != ToolLoadingModeDeferred || webFetch.Loading.Reason != "web_access" {
		t.Fatalf("web_fetch contract = scope:%q loading:%+v", webFetch.ResourceScope, webFetch.Loading)
	}
	if webFetch.Execution.ParallelPolicy != ParallelPolicyReadOnly {
		t.Fatalf("web_fetch parallel = %q, want %q", webFetch.Execution.ParallelPolicy, ParallelPolicyReadOnly)
	}
	webSearch, ok := ConfiguredLocalSpec(cfg, "web_search")
	if !ok {
		t.Fatal("web_search spec missing")
	}
	if webSearch.ResourceScope != ResourceScopeWeb || webSearch.Loading.Mode != ToolLoadingModeDeferred || webSearch.Loading.Reason != "web_access" {
		t.Fatalf("web_search contract = scope:%q loading:%+v", webSearch.ResourceScope, webSearch.Loading)
	}
	if webSearch.Execution.ParallelPolicy != ParallelPolicyReadOnly {
		t.Fatalf("web_search parallel = %q, want %q", webSearch.Execution.ParallelPolicy, ParallelPolicyReadOnly)
	}
	browser, ok := ConfiguredLocalSpec(cfg, "browser")
	if !ok {
		t.Fatal("browser spec missing")
	}
	if browser.ResourceScope != ResourceScopeBrowser || browser.Loading.Mode != ToolLoadingModeDeferred || browser.Loading.Reason != "web_access" {
		t.Fatalf("browser contract = scope:%q loading:%+v", browser.ResourceScope, browser.Loading)
	}
	if browser.Execution.ParallelPolicy != ParallelPolicyNeverParallel {
		t.Fatalf("browser parallel = %q, want %q", browser.Execution.ParallelPolicy, ParallelPolicyNeverParallel)
	}
}

func TestToolContractAcceptsNativeDeveloperToolScopes(t *testing.T) {
	tests := []struct {
		name  string
		scope ResourceScope
	}{
		{"artifact", ResourceScopeArtifact},
		{"operator", ResourceScopeOperator},
		{"web", ResourceScopeWeb},
		{"browser", ResourceScopeBrowser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := ToolContract{
				Name:          tt.name + "_tool",
				Source:        "local",
				Kind:          ToolKindNative,
				Category:      ToolCategoryExecute,
				ResourceScope: tt.scope,
				Profiles:      []ToolProfile{ToolProfileRun},
				PlanPolicy:    PlanPolicyRequireActivePlan,
				Loading:       EagerLoadingPolicy(),
				Execution: ToolExecutionPolicy{
					ParallelPolicy: ParallelPolicyNeverParallel,
				},
			}
			if err := contract.Validate(); err != nil {
				t.Fatalf("validate contract: %v", err)
			}
		})
	}
}

func TestToolContractRejectsUnknownResourceScope(t *testing.T) {
	contract := ToolContract{
		Name:          "bad_scope_tool",
		Source:        "local",
		Kind:          ToolKindNative,
		Category:      ToolCategoryRead,
		ResourceScope: ResourceScope("bad"),
		Profiles:      []ToolProfile{ToolProfileRun},
		PlanPolicy:    PlanPolicyNone,
		Loading:       EagerLoadingPolicy(),
		Execution: ToolExecutionPolicy{
			ParallelPolicy: ParallelPolicyReadOnly,
		},
	}
	if err := contract.Validate(); err == nil {
		t.Fatal("expected unknown resource scope error")
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
