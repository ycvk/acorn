package tooling

import (
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
)

type ToolKind string

const (
	ToolKindNative ToolKind = "native"
	ToolKindMemory ToolKind = "memory"
	ToolKindSkill  ToolKind = "skill"
	ToolKindMCP    ToolKind = "mcp"
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

type ParallelPolicy string

const (
	ParallelPolicyReadOnly ParallelPolicy = "read_only"
	ParallelPolicySerial   ParallelPolicy = "serial"
)

func ParseParallelPolicy(raw string) (ParallelPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "readonly", "read_only":
		return ParallelPolicyReadOnly, nil
	case "serial":
		return ParallelPolicySerial, nil
	default:
		return "", errUnknownParallelPolicy(raw)
	}
}

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
			Name:     name,
			Source:   "local",
			Kind:     ToolKindNative,
			Category: ToolCategoryInspect,
			Loading:  EagerLoadingPolicy(),
			Execution: ToolExecutionPolicy{
				ParallelPolicy: ParallelPolicyReadOnly,
			},
		},
	}
	switch name {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	case "git_summary":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "artifact_read", "artifact_list":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
	case "artifact_write":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "ask_operator":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "web_fetch", "web_search":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
	case "browser":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "create_file", "replace_span", "apply_unified_patch":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
		if name == "apply_unified_patch" {
			spec.Execution.PathArg = "paths"
		} else {
			spec.Execution.PathArg = "path"
		}
	case "multi_edit":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "rollback_workspace_checkpoint":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "run_command":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "run_verification":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	default:
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
	}
	if enabled {
		spec.Health = healthyTool("")
	} else {
		spec.Health = disabledTool(name + " disabled in config")
	}
	return spec
}
