package tooling

import (
	"slices"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
)

type ToolKind string

const (
	ToolKindNative      ToolKind = "native"
	ToolKindMemory      ToolKind = "memory"
	ToolKindSkill       ToolKind = "skill"
	ToolKindMCP         ToolKind = "mcp"
	ToolKindMCPResource ToolKind = "mcp_resource"
	ToolKindMCPPrompt   ToolKind = "mcp_prompt"
)

type ToolCategory string

const (
	ToolCategoryRead        ToolCategory = "read"
	ToolCategoryWrite       ToolCategory = "write"
	ToolCategoryExecute     ToolCategory = "execute"
	ToolCategoryInspect     ToolCategory = "inspect"
	ToolCategoryMemory      ToolCategory = "memory"
	ToolCategorySkill       ToolCategory = "skill"
	ToolCategoryIntegration ToolCategory = "integration"
)

type ToolProfile string

const (
	ToolProfileRun   ToolProfile = "run"
	ToolProfileServe ToolProfile = "serve"
)

type ParallelPolicy string

const (
	ParallelPolicyReadOnly      ParallelPolicy = "read_only"
	ParallelPolicyWriteScoped   ParallelPolicy = "write_scoped"
	ParallelPolicyNeverParallel ParallelPolicy = "never_parallel"
)

func ParseParallelPolicy(raw string) (ParallelPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "readonly", "read_only":
		return ParallelPolicyReadOnly, nil
	case "write_scoped":
		return ParallelPolicyWriteScoped, nil
	case "never_parallel":
		return ParallelPolicyNeverParallel, nil
	default:
		return "", errUnknownParallelPolicy(raw)
	}
}

type PlanPolicy string

const (
	PlanPolicyNone              PlanPolicy = "none"
	PlanPolicyRequireActivePlan PlanPolicy = "require_active_plan"
)

type FactPolicy string

const (
	FactPolicyAuto     FactPolicy = "auto"
	FactPolicySuppress FactPolicy = "suppress"
)

type ResourceScope string

const (
	ResourceScopeWorkspaceFile    ResourceScope = "workspace_file"
	ResourceScopeWorkspaceCommand ResourceScope = "workspace_command"
	ResourceScopeMemory           ResourceScope = "memory"
	ResourceScopeSkill            ResourceScope = "skill"
	ResourceScopeMCP              ResourceScope = "mcp"
	ResourceScopeArtifact         ResourceScope = "artifact"
	ResourceScopeOperator         ResourceScope = "operator"
	ResourceScopeWeb              ResourceScope = "web"
	ResourceScopeBrowser          ResourceScope = "browser"
)

type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDisabled HealthState = "disabled"
	HealthStateDegraded HealthState = "degraded"
)

type ToolHealth struct {
	State  HealthState
	Reason string
}

type ToolSpec struct {
	ToolContract
	Tool   einotool.BaseTool
	Health ToolHealth
}

func (s ToolSpec) Enabled() bool {
	return s.Health.State != HealthStateDisabled
}

func (s ToolSpec) HasProfile(profile ToolProfile) bool {
	if strings.TrimSpace(string(profile)) == "" {
		return true
	}
	return slices.Contains(s.Profiles, profile)
}

func healthyTool(reason string) ToolHealth {
	return ToolHealth{State: HealthStateHealthy, Reason: strings.TrimSpace(reason)}
}

func disabledTool(reason string) ToolHealth {
	return ToolHealth{State: HealthStateDisabled, Reason: strings.TrimSpace(reason)}
}

// localToolDef declares a static local tool plus whether the given config
// enables it. localToolDefs is the single source of truth for the static local
// toolset: both ConfiguredLocalSpecs and ConfiguredLocalSpec derive from it, so
// the tool list and its enable rules live in exactly one place (no parallel
// switch to drift out of sync).
type localToolDef struct {
	name    string
	enabled bool
}

func localToolDefs(cfg *config.Config) []localToolDef {
	mutation := !cfg.Tools.Mutation.Disabled
	runCommand := !cfg.Tools.RunCommand.Disabled
	return []localToolDef{
		{"read_file", true},
		{"list_files", true},
		{"search_text", true},
		{"inspect_git_status", true},
		{"inspect_git_diff", true},
		{"git_summary", true},
		{"artifact_write", true},
		{"artifact_read", true},
		{"artifact_list", true},
		{"ask_operator", true},
		{"web_fetch", true},
		{"web_search", true},
		{"browser", true},
		{"create_file", mutation},
		{"replace_span", mutation},
		{"apply_unified_patch", mutation},
		{"multi_edit", mutation},
		{"rollback_workspace_checkpoint", mutation},
		{"run_command", runCommand},
		{"run_verification", runCommand},
	}
}

func ConfiguredLocalSpecs(cfg *config.Config) []ToolSpec {
	if cfg == nil {
		return nil
	}
	defs := localToolDefs(cfg)
	specs := make([]ToolSpec, 0, len(defs))
	for _, def := range defs {
		specs = append(specs, configuredLocalSpec(def.name, def.enabled))
	}
	return specs
}

func ConfiguredLocalSpec(cfg *config.Config, name string) (ToolSpec, bool) {
	if cfg == nil {
		return ToolSpec{}, false
	}
	name = strings.TrimSpace(name)
	for _, def := range localToolDefs(cfg) {
		if def.name == name {
			return configuredLocalSpec(name, def.enabled), true
		}
	}
	return ToolSpec{}, false
}

func configuredLocalSpec(name string, enabled bool) ToolSpec {
	spec := ToolSpec{
		ToolContract: ToolContract{
			Name:          name,
			Source:        "local",
			Kind:          ToolKindNative,
			Category:      ToolCategoryInspect,
			ResourceScope: ResourceScopeWorkspaceFile,
			Profiles:      []ToolProfile{ToolProfileRun, ToolProfileServe},
			PlanPolicy:    PlanPolicyNone,
			Loading:       EagerLoadingPolicy(),
			Execution: ToolExecutionPolicy{
				ParallelPolicy: ParallelPolicyReadOnly,
			},
		},
	}
	switch name {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
		spec.PlanPolicy = PlanPolicyNone
	case "git_summary":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyNone
	case "artifact_read", "artifact_list":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeArtifact
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.PlanPolicy = PlanPolicyNone
	case "artifact_write":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.ResourceScope = ResourceScopeArtifact
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyNone
	case "ask_operator":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.ResourceScope = ResourceScopeOperator
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyNone
	case "web_fetch":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeWeb
		spec.Profiles = []ToolProfile{ToolProfileRun}
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.PlanPolicy = PlanPolicyNone
	case "web_search":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeWeb
		spec.Profiles = []ToolProfile{ToolProfileRun}
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.PlanPolicy = PlanPolicyNone
	case "browser":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.ResourceScope = ResourceScopeBrowser
		spec.Profiles = []ToolProfile{ToolProfileRun}
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyNone
	case "create_file", "replace_span", "apply_unified_patch":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyWriteScoped
		if name == "apply_unified_patch" {
			spec.Execution.PathArg = "paths"
		} else {
			spec.Execution.PathArg = "path"
		}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "multi_edit":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "rollback_workspace_checkpoint":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "run_command":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.ResourceScope = ResourceScopeWorkspaceCommand
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "run_verification":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.ResourceScope = ResourceScopeWorkspaceCommand
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	default:
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.PlanPolicy = PlanPolicyNone
	}
	if enabled {
		spec.Health = healthyTool("")
	} else {
		spec.Health = disabledTool(name + " disabled in config")
	}
	return spec
}
