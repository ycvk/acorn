package tools

import (
	"encoding/gob"
	"errors"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/domain"
)

func buildWorkspaceTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if cfg.Workspace == nil {
		return nil, nil
	}
	readTools, err := buildReadTools(cfg)
	if err != nil {
		return nil, err
	}
	gitTools, err := buildGitTools(cfg)
	if err != nil {
		return nil, err
	}
	return append(readTools, gitTools...), nil
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
	searchTool, err := buildSearchTextTool(ws)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{readTool, listTool, searchTool}, nil
}

func buildGitTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	ws := cfg.Workspace
	gitStatusTool, err := buildInspectGitStatusTool(ws)
	if err != nil {
		return nil, err
	}
	gitDiffTool, err := buildInspectGitDiffTool(ws)
	if err != nil {
		return nil, err
	}
	gitSummaryTool, err := buildGitSummaryTool(ws, cfg.ArtifactService, cfg.ArtifactContext)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{gitStatusTool, gitDiffTool, gitSummaryTool}, nil
}

func buildMutationTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if !cfg.MutationEnabled {
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
	patchTool, err := buildApplyUnifiedPatchTool(ws)
	if err != nil {
		return nil, err
	}
	multiEditTool, err := buildMultiEditTool(ws)
	if err != nil {
		return nil, err
	}
	rollbackTool, err := buildRollbackWorkspaceCheckpointTool(ws)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{createTool, replaceTool, patchTool, multiEditTool, rollbackTool}, nil
}

func buildRunCommandTools(cfg CatalogConfig) ([]einotool.BaseTool, error) {
	if !cfg.RunCommandEnabled {
		return nil, nil
	}
	runTool, err := buildRunCommandTool(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if cfg.ArtifactService == nil {
		return []einotool.BaseTool{runTool}, nil
	}
	verifyTool, err := buildRunVerificationTool(cfg.Workspace, cfg.ArtifactService, cfg.ArtifactContext)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{runTool, verifyTool}, nil
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
	Workspace         WorkspaceView
	MutationEnabled   bool
	RunCommandEnabled bool
	ArtifactService   ArtifactService
	ArtifactContext   domain.ToolCallContextBridge
	OperatorStore     OperatorQuestionStore
	OperatorContext   domain.ToolCallContextBridge
	WebFetchService   WebFetchService
	WebSearchService  WebSearchService
	BrowserService    BrowserService
}

type LocalCatalog struct {
	Tools []einotool.BaseTool
}

func init() {
	gob.Register(RunCommandInput{})
	gob.Register(AskOperatorState{})
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

func BuildCatalog(cfg CatalogConfig, extraTools []einotool.BaseTool) (*LocalCatalog, error) {
	if cfg.Workspace == nil && (cfg.MutationEnabled || cfg.RunCommandEnabled) {
		return nil, errors.New("workspace is required when mutation or run_command tools are enabled")
	}
	items := make([]einotool.BaseTool, 0, 10+len(extraTools))
	groups := []func() ([]einotool.BaseTool, error){
		func() ([]einotool.BaseTool, error) { return buildWorkspaceTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildMutationTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildRunCommandTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildArtifactServiceTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildOperatorTool(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWebFetchToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWebSearchToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildBrowserToolEntry(cfg) },
	}
	for _, group := range groups {
		built, err := group()
		if err != nil {
			return nil, err
		}
		items = append(items, built...)
	}
	items = append(items, extraTools...)
	return &LocalCatalog{Tools: items}, nil
}
