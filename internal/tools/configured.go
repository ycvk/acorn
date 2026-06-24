package tools

import (
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/port"
)

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

func ConfiguredLocalSpecs(cfg *config.Config) []port.ToolSpec {
	if cfg == nil {
		return nil
	}
	defs := localToolDefs(cfg)
	specs := make([]port.ToolSpec, 0, len(defs))
	for _, def := range defs {
		specs = append(specs, configuredLocalSpec(def.name, def.enabled))
	}
	return specs
}

func ConfiguredLocalSpec(cfg *config.Config, name string) (port.ToolSpec, bool) {
	if cfg == nil {
		return port.ToolSpec{}, false
	}
	name = strings.TrimSpace(name)
	for _, def := range localToolDefs(cfg) {
		if def.name == name {
			return configuredLocalSpec(name, def.enabled), true
		}
	}
	return port.ToolSpec{}, false
}

func configuredLocalSpec(name string, enabled bool) port.ToolSpec {
	spec := port.ToolSpec{
		ToolContract: port.ToolContract{
			Name:      name,
			Source:    "local",
			Kind:      port.ToolKindNative,
			Category:  port.ToolCategoryInspect,
			Loading:   port.EagerLoadingPolicy(),
			Execution: port.ToolExecutionPolicy{ParallelPolicy: port.ParallelPolicyReadOnly},
		},
	}
	switch name {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryRead
		spec.Execution.ParallelPolicy = port.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	case "git_summary":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryInspect
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "artifact_read", "artifact_list":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryRead
		spec.Execution.ParallelPolicy = port.ParallelPolicyReadOnly
	case "artifact_write":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryWrite
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "ask_operator":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryIntegration
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "web_fetch", "web_search":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryRead
		spec.Loading = port.DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = port.ParallelPolicyReadOnly
	case "browser":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryIntegration
		spec.Loading = port.DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "create_file", "replace_span", "apply_unified_patch":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryWrite
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
		if name == "apply_unified_patch" {
			spec.Execution.PathArg = "paths"
		} else {
			spec.Execution.PathArg = "path"
		}
	case "multi_edit":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryWrite
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "rollback_workspace_checkpoint":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryWrite
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "run_command":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryExecute
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "run_verification":
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryExecute
		spec.Execution.ParallelPolicy = port.ParallelPolicySerial
	default:
		spec.Kind = port.ToolKindNative
		spec.Category = port.ToolCategoryInspect
		spec.Execution.ParallelPolicy = port.ParallelPolicyReadOnly
	}
	if enabled {
		spec.Health = port.ToolHealth{State: port.HealthStateHealthy}
	} else {
		spec.Health = port.ToolHealth{State: port.HealthStateDisabled, Reason: name + " disabled in config"}
	}
	return spec
}
