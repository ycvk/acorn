package tools

import (
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
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

func ConfiguredLocalSpecs(cfg *config.Config) []core.ToolSpec {
	if cfg == nil {
		return nil
	}
	defs := localToolDefs(cfg)
	specs := make([]core.ToolSpec, 0, len(defs))
	for _, def := range defs {
		specs = append(specs, configuredLocalSpec(def.name, def.enabled))
	}
	return specs
}

func ConfiguredLocalSpec(cfg *config.Config, name string) (core.ToolSpec, bool) {
	if cfg == nil {
		return core.ToolSpec{}, false
	}
	name = strings.TrimSpace(name)
	for _, def := range localToolDefs(cfg) {
		if def.name == name {
			return configuredLocalSpec(name, def.enabled), true
		}
	}
	return core.ToolSpec{}, false
}

func configuredLocalSpec(name string, enabled bool) core.ToolSpec {
	spec := core.ToolSpec{
		ToolContract: core.ToolContract{
			Name:      name,
			Source:    "local",
			Kind:      core.ToolKindNative,
			Category:  core.ToolCategoryInspect,
			Loading:   core.EagerLoadingPolicy(),
			Execution: core.ToolExecutionPolicy{ParallelPolicy: core.ParallelPolicyReadOnly},
		},
	}
	switch name {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
		spec.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	case "git_summary":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryInspect
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "artifact_read", "artifact_list":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
		spec.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
	case "artifact_write":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryWrite
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "ask_operator":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "web_fetch", "web_search":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
		spec.Loading = core.DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
	case "browser":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
		spec.Loading = core.DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "create_file", "replace_span", "apply_unified_patch":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryWrite
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
		if name == "apply_unified_patch" {
			spec.Execution.PathArg = "paths"
		} else {
			spec.Execution.PathArg = "path"
		}
	case "multi_edit":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryWrite
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "rollback_workspace_checkpoint":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryWrite
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "run_command":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryExecute
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "run_verification":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryExecute
		spec.Execution.ParallelPolicy = core.ParallelPolicySerial
	default:
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryInspect
		spec.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
	}
	if enabled {
		spec.Health = core.ToolHealth{State: core.HealthStateHealthy}
	} else {
		spec.Health = core.ToolHealth{State: core.HealthStateDisabled, Reason: name + " disabled in config"}
	}
	return spec
}
