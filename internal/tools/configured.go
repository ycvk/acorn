package tools

import (
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// localToolNames is the single source of truth for the static local toolset:
// ConfiguredLocalSpecs, ConfiguredLocalSpec and RegisterNativeTools all derive
// from it, so the tool list lives in exactly one place.
var localToolNames = []string{
	"artifact_write",
	"artifact_read",
	"artifact_list",
	"ask_operator",
	"keep",
	"think",
	"schedule_wake",
	"settle",
	"recall",
	"memory_read",
	"memory_correct",
	"memory_forget",
	"concern",
	"notify_owner",
	"knowledge_write",
	"knowledge_edit",
	"knowledge_read",
	"knowledge_search",
	"knowledge_list",
	"skill_write",
	"skill_retire",
	"skill_inspect",
	"skill_import",
	"watch_create",
	"watch_update",
	"watch_list",
	"show_view",
	"update_view",
	"web_fetch",
	"web_search",
	"shell",
	"files",
	"send_file",
}

// runScopedTool reports whether the runtime toolset builds the tool for each
// run from live services (web access, workstation) instead of the registry
// resolving it from wire-time dependencies.
func runScopedTool(name string) bool {
	switch name {
	case "web_fetch", "web_search", "shell", "files", "send_file":
		return true
	}
	return false
}

func ConfiguredLocalSpecs() []core.ToolSpec {
	specs := make([]core.ToolSpec, 0, len(localToolNames))
	for _, name := range localToolNames {
		specs = append(specs, configuredLocalSpec(name))
	}
	return specs
}

func ConfiguredLocalSpec(name string) (core.ToolSpec, bool) {
	name = strings.TrimSpace(name)
	for _, candidate := range localToolNames {
		if candidate == name {
			return configuredLocalSpec(name), true
		}
	}
	return core.ToolSpec{}, false
}

func configuredLocalSpec(name string) core.ToolSpec {
	spec := core.ToolSpec{
		ToolContract: core.ToolContract{
			Name:     name,
			Source:   "local",
			Kind:     core.ToolKindNative,
			Category: core.ToolCategoryInspect,
			Loading:  core.EagerLoadingPolicy(),
		},
	}
	switch name {
	case "artifact_read", "artifact_list":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
	case "artifact_write", "knowledge_write", "knowledge_edit", "skill_write", "skill_retire", "skill_import", "watch_create", "watch_update":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryWrite
	case "ask_operator", "notify_owner", "send_file":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
	case "keep", "think", "schedule_wake", "settle", "memory_correct", "memory_forget", "concern":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryMemory
	case "recall", "memory_read", "knowledge_read", "knowledge_search", "knowledge_list", "skill_inspect", "watch_list":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
	case "web_fetch", "web_search":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
	case "shell", "files":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
		spec.Loading = core.DeferredLoadingPolicy("workstation")
	case "show_view", "update_view":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
		spec.Loading = core.DeferredLoadingPolicy("views")
	default:
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryInspect
	}
	spec.Health = core.ToolHealth{State: core.HealthStateHealthy}
	return spec
}
