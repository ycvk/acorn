package tooling

import (
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
	ResourceScopeProcess          ResourceScope = "process"
	ResourceScopeArtifact         ResourceScope = "artifact"
	ResourceScopeOperator         ResourceScope = "operator"
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
	for _, candidate := range s.Profiles {
		if candidate == profile {
			return true
		}
	}
	return false
}

func healthyTool(reason string) ToolHealth {
	return ToolHealth{State: HealthStateHealthy, Reason: strings.TrimSpace(reason)}
}

func disabledTool(reason string) ToolHealth {
	return ToolHealth{State: HealthStateDisabled, Reason: strings.TrimSpace(reason)}
}

func ConfiguredLocalSpecs(cfg *config.Config) []ToolSpec {
	if cfg == nil {
		return nil
	}
	return []ToolSpec{
		configuredLocalSpec("read_file", true),
		configuredLocalSpec("list_files", true),
		configuredLocalSpec("search_text", true),
		configuredLocalSpec("inspect_git_status", true),
		configuredLocalSpec("inspect_git_diff", true),
		configuredLocalSpec("artifact_write", true),
		configuredLocalSpec("artifact_read", true),
		configuredLocalSpec("artifact_list", true),
		configuredLocalSpec("terminal_session_start", true),
		configuredLocalSpec("terminal_session_write", true),
		configuredLocalSpec("terminal_session_read", true),
		configuredLocalSpec("terminal_session_signal", true),
		configuredLocalSpec("terminal_session_close", true),
		configuredLocalSpec("terminal_session_list", true),
		configuredLocalSpec("process_status", true),
		configuredLocalSpec("ask_operator", true),
		configuredLocalSpec("create_file", !cfg.Tools.Mutation.Disabled),
		configuredLocalSpec("replace_span", !cfg.Tools.Mutation.Disabled),
		configuredLocalSpec("apply_unified_patch", !cfg.Tools.Mutation.Disabled),
		configuredLocalSpec("rollback_workspace_checkpoint", !cfg.Tools.Mutation.Disabled),
		configuredLocalSpec("run_command", !cfg.Tools.RunCommand.Disabled),
	}
}

func ConfiguredLocalSpec(cfg *config.Config, name string) (ToolSpec, bool) {
	if cfg == nil {
		return ToolSpec{}, false
	}
	switch strings.TrimSpace(name) {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff", "artifact_write", "artifact_read", "artifact_list", "terminal_session_start", "terminal_session_write", "terminal_session_read", "terminal_session_signal", "terminal_session_close", "terminal_session_list", "process_status", "ask_operator":
		return configuredLocalSpec(strings.TrimSpace(name), true), true
	case "create_file", "replace_span":
		return configuredLocalSpec(strings.TrimSpace(name), !cfg.Tools.Mutation.Disabled), true
	case "apply_unified_patch":
		return configuredLocalSpec("apply_unified_patch", !cfg.Tools.Mutation.Disabled), true
	case "rollback_workspace_checkpoint":
		return configuredLocalSpec("rollback_workspace_checkpoint", !cfg.Tools.Mutation.Disabled), true
	case "run_command":
		return configuredLocalSpec("run_command", !cfg.Tools.RunCommand.Disabled), true
	default:
		return ToolSpec{}, false
	}
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
			FactPolicy:    FactPolicyAuto,
			Loading:       EagerLoadingPolicy(),
			Execution: ToolExecutionPolicy{
				ParallelPolicy: ParallelPolicyReadOnly,
				SideEffects:    []ToolSideEffect{ToolSideEffectReadWorkspace},
			},
			Result:     InlineResultPolicy(0),
			Boundary:   ToolResultBoundaryPolicy(),
			Projection: ActivityProjectionPolicy(),
		},
	}
	switch name {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectReadWorkspace}
		spec.PlanPolicy = PlanPolicyNone
	case "artifact_read", "artifact_list":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeArtifact
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectArtifactRead}
		spec.PlanPolicy = PlanPolicyNone
	case "artifact_write":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.ResourceScope = ResourceScopeArtifact
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectArtifactWrite}
		spec.PlanPolicy = PlanPolicyNone
	case "terminal_session_read", "terminal_session_list", "process_status":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.ResourceScope = ResourceScopeProcess
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectProcessRead}
		spec.PlanPolicy = PlanPolicyNone
	case "terminal_session_start":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.ResourceScope = ResourceScopeProcess
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectProcessStart}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "terminal_session_write":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.ResourceScope = ResourceScopeProcess
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectProcessWrite}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "terminal_session_signal", "terminal_session_close":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.ResourceScope = ResourceScopeProcess
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectProcessSignal}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "ask_operator":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.ResourceScope = ResourceScopeOperator
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectOperatorInteraction}
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
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectWriteWorkspace}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "rollback_workspace_checkpoint":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectWriteWorkspace}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	case "run_command":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.ResourceScope = ResourceScopeWorkspaceCommand
		spec.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectRunCommand}
		spec.PlanPolicy = PlanPolicyRequireActivePlan
	default:
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.ResourceScope = ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.SideEffects = []ToolSideEffect{ToolSideEffectReadWorkspace}
		spec.PlanPolicy = PlanPolicyNone
	}
	if enabled {
		spec.Health = healthyTool("")
	} else {
		spec.Health = disabledTool(name + " disabled in config")
	}
	return spec
}
