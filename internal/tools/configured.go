package tools

import (
	"slices"
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
	"plan",
	"keep",
	"think",
	"schedule_wake",
	"settle",
	"goal",
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
	"news_sections",
	"news_source",
	"news_search",
	"feed_discover",
	"show_view",
	"update_view",
	"web_fetch",
	"web_search",
	"shell",
	"job",
	"follow_up",
	"files",
	"send_file",
	"delegate",
	"submit",
}

// runScopedTool reports whether the runtime toolset builds the tool for each
// run from live services (web access, workstation, the run's sub-runs)
// instead of the registry resolving it from wire-time dependencies.
func runScopedTool(name string) bool {
	switch name {
	case "web_fetch", "web_search", "shell", "job", "follow_up", "files", "send_file", "delegate", "submit":
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
	if slices.Contains(localToolNames, name) {
		return configuredLocalSpec(name), true
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
		},
	}
	switch name {
	case "artifact_read", "artifact_list":
		spec.Category = core.ToolCategoryRead
	case "artifact_write", "knowledge_write", "knowledge_edit", "skill_write", "skill_retire", "skill_import", "watch_create", "watch_update", "news_sections", "news_source":
		spec.Category = core.ToolCategoryWrite
	case "ask_operator", "notify_owner", "delegate", "submit":
		spec.Category = core.ToolCategoryIntegration
	case "keep", "think", "schedule_wake", "settle", "goal", "memory_correct", "memory_forget", "concern":
		spec.Category = core.ToolCategoryMemory
	case "recall", "memory_read", "knowledge_read", "knowledge_search", "knowledge_list", "skill_inspect", "watch_list", "news_search", "feed_discover":
		spec.Category = core.ToolCategoryRead
	case "web_fetch", "web_search":
		spec.Category = core.ToolCategoryRead
	case "shell", "job", "follow_up", "files", "send_file", "show_view", "update_view":
		spec.Category = core.ToolCategoryIntegration
	}
	spec.Health = core.ToolHealth{State: core.HealthStateHealthy}
	return spec
}
