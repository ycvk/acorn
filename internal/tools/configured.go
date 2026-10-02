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
	"search_runs",
	"worldstate_update",
	"worldstate_load",
	"web_fetch",
	"web_search",
	"browser",
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
	case "artifact_write":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryWrite
	case "ask_operator":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
	case "worldstate_update":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryMemory
	case "web_fetch", "web_search":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryRead
		spec.Loading = core.DeferredLoadingPolicy("web_access")
	case "browser":
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryIntegration
		spec.Loading = core.DeferredLoadingPolicy("web_access")
	default:
		spec.Kind = core.ToolKindNative
		spec.Category = core.ToolCategoryInspect
	}
	spec.Health = core.ToolHealth{State: core.HealthStateHealthy}
	return spec
}
