package tools

import (
	"encoding/gob"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
)

func init() {
	gob.Register(AskOperatorState{})
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

// BuildWorkspaceTools builds the file tools (read_file, list_files,
// create_file, replace_span) scoped to ws.
func BuildWorkspaceTools(ws WorkspaceView) ([]einotool.BaseTool, error) {
	if ws == nil {
		return nil, errors.New("BuildWorkspaceTools: workspace is required")
	}
	readTool, err := buildReadFileTool(ws)
	if err != nil {
		return nil, err
	}
	listTool, err := buildListFilesTool(ws)
	if err != nil {
		return nil, err
	}
	createTool, err := buildCreateFileTool(ws)
	if err != nil {
		return nil, err
	}
	replaceTool, err := buildReplaceSpanTool(ws)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{readTool, listTool, createTool, replaceTool}, nil
}

// WebToolsConfig carries the per-run services behind the deferred web tools.
// Fetch is always required. Search and Browser depend on operator
// configuration: a nil service disables its tool, and the matching disabled
// reason is then required so the capability snapshot (acorn doctor) can say
// why the tool is unavailable.
type WebToolsConfig struct {
	ArtifactService       core.ArtifactService
	ArtifactContext       core.ToolCallContextBridge
	Fetch                 WebFetchService
	Search                WebSearchService
	SearchDisabledReason  string
	Browser               BrowserService
	BrowserDisabledReason string
}

// BuildWebToolSpecs returns the specs for web_fetch, web_search and browser.
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

	if cfg.Browser == nil {
		spec, err := disabledLocalSpec("browser", cfg.BrowserDisabledReason)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	} else {
		browserTool, err := buildBrowserTool(cfg.Browser, cfg.ArtifactService, cfg.ArtifactContext)
		if err != nil {
			return nil, err
		}
		specs = append(specs, enabledLocalSpec("browser", browserTool))
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
