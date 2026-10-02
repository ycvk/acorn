package tools

import (
	"encoding/gob"
	"errors"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
)

func buildWorkspaceTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.Workspace == nil {
		return nil, nil
	}
	return buildReadTools(cfg)
}

func buildReadTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	ws := cfg.Workspace
	readTool, err := buildReadFileTool(ws)
	if err != nil {
		return nil, err
	}
	listTool, err := buildListFilesTool(ws)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{readTool, listTool}, nil
}

func buildMutationTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.Workspace == nil {
		return nil, nil
	}
	ws := cfg.Workspace
	createTool, err := buildCreateFileTool(ws)
	if err != nil {
		return nil, err
	}
	replaceTool, err := buildReplaceSpanTool(ws)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{createTool, replaceTool}, nil
}

func buildArtifactServiceTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.ArtifactService == nil {
		return nil, nil
	}
	return buildArtifactTools(cfg.ArtifactService, cfg.ArtifactContext)
}

func buildOperatorTool(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.OperatorStore == nil {
		return nil, nil
	}
	operatorTool, err := buildAskOperatorTool(cfg.OperatorStore, cfg.OperatorContext)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{operatorTool}, nil
}

func buildWebFetchToolEntry(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.WebFetchService == nil {
		return nil, nil
	}
	if cfg.ArtifactService == nil {
		return nil, errors.New("artifact service is required when web_fetch is enabled")
	}
	webFetchTool, err := buildWebFetchTool(cfg.WebFetchService, cfg.ArtifactService, cfg.ArtifactContext)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{webFetchTool}, nil
}

func buildWebSearchToolEntry(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.WebSearchService == nil {
		return nil, nil
	}
	if cfg.ArtifactService == nil {
		return nil, errors.New("artifact service is required when web_search is enabled")
	}
	webSearchTool, err := buildWebSearchTool(cfg.WebSearchService, cfg.ArtifactService, cfg.ArtifactContext)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{webSearchTool}, nil
}

func buildBrowserToolEntry(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.BrowserService == nil {
		return nil, nil
	}
	if cfg.ArtifactService == nil {
		return nil, errors.New("artifact service is required when browser is enabled")
	}
	browserTool, err := buildBrowserTool(cfg.BrowserService, cfg.ArtifactService, cfg.ArtifactContext)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{browserTool}, nil
}

type CatalogConfig struct {
	// Workspace scopes the file tools; only the memory root sets it.
	Workspace         WorkspaceView
	ArtifactService   core.ArtifactService
	ArtifactContext   core.ToolCallContextBridge
	OperatorStore     OperatorQuestionStore
	RunSearchStore    RunSearchStore
	WorldStateUpdater WorldStateUpdater
	OperatorContext   core.ToolCallContextBridge
	WebFetchService   WebFetchService
	WebSearchService  WebSearchService
	BrowserService    BrowserService
}

type LocalCatalog struct {
	Tools []einotool.BaseTool
}

func init() {
	gob.Register(AskOperatorState{})
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

func BuildCatalog(cfg CatalogConfig) (*LocalCatalog, error) {
	items := make([]einotool.BaseTool, 0, 10)
	groups := []func() ([]einotool.BaseTool, error){
		func() ([]einotool.BaseTool, error) { return buildWorkspaceTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildMutationTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildArtifactServiceTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildOperatorTool(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWebFetchToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWebSearchToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildBrowserToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWorldStateTools(cfg) },
	}
	for _, group := range groups {
		built, err := group()
		if err != nil {
			return nil, err
		}
		items = append(items, built...)
	}
	return &LocalCatalog{Tools: items}, nil
}

func buildWorldStateTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.WorldStateUpdater == nil {
		return nil, nil
	}
	updateTool, err := buildWorldStateUpdateTool(cfg.WorldStateUpdater)
	if err != nil {
		return nil, err
	}
	loadTool, err := buildWorldStateLoadTool(cfg.WorldStateUpdater)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{updateTool, loadTool}, nil
}
