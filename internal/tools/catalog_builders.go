package tools

import (
	"encoding/gob"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
)

func init() {
	gob.Register(AskOperatorState{})
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

// WebToolsConfig carries the per-run services behind the web tools.
// Fetch is always required. Search depends on operator configuration: a nil
// service disables its tool, and the disabled reason is then required so the
// capability snapshot (acorn doctor) can say why the tool is unavailable.
type WebToolsConfig struct {
	ArtifactService      core.ArtifactService
	ArtifactContext      core.ToolCallContextBridge
	Fetch                WebFetchService
	Search               WebSearchService
	SearchDisabledReason string
}

// BuildWebToolSpecs returns the specs for web_fetch and web_search.
// An unconfigured optional tool is returned as a disabled spec carrying its
// reason; it never reaches the model.
func BuildWebToolSpecs(cfg WebToolsConfig) ([]core.ToolSpec, error) {
	fetchTool, err := buildWebFetchTool(cfg.Fetch, cfg.ArtifactService, cfg.ArtifactContext)
	if err != nil {
		return nil, err
	}
	specs := []core.ToolSpec{enabledLocalSpec("web_fetch", fetchTool)}

	if cfg.Search == nil {
		spec, err := disabledLocalSpec("web_search", cfg.SearchDisabledReason)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	} else {
		searchTool, err := buildWebSearchTool(cfg.Search, cfg.ArtifactService, cfg.ArtifactContext)
		if err != nil {
			return nil, err
		}
		specs = append(specs, enabledLocalSpec("web_search", searchTool))
	}
	return specs, nil
}

func enabledLocalSpec(name string, tool einotool.BaseTool) core.ToolSpec {
	spec := configuredLocalSpec(name)
	spec.Tool = tool
	return spec
}

func disabledLocalSpec(name, reason string) (core.ToolSpec, error) {
	if reason == "" {
		return core.ToolSpec{}, fmt.Errorf("%s: service is nil and no disabled reason is given", name)
	}
	spec := configuredLocalSpec(name)
	spec.Health = core.ToolHealth{State: core.HealthStateDisabled, Reason: reason}
	return spec, nil
}
