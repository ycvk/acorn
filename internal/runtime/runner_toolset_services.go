package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/runtime/toolset"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workingstate"
)

type toolsetWebServices struct {
	fetch  *webaccess.FetchService
	search *webaccess.SearchService
}

type auxTools struct {
	checkpoint []einotool.BaseTool
	memory     []einotool.BaseTool
	skill      []einotool.BaseTool
	lifecycle  []einotool.BaseTool
}

func (f *RunnerFactory) buildToolsetWebServices() (toolsetWebServices, error) {
	cfg := f.deps.Config.WebAccess
	fetch, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        cfg.UserAgent,
		Timeout:          time.Duration(cfg.TimeoutSeconds) * time.Second,
		MaxResponseBytes: cfg.MaxResponseBytes,
		Policy:           webaccess.URLPolicy{AllowPrivateNetworks: cfg.AllowPrivateNetworks},
	})
	if err != nil {
		return toolsetWebServices{}, fmt.Errorf("web fetch service: %w", err)
	}
	search, err := webaccess.NewSearchService(webaccess.SearchConfig{
		APIKey:           cfg.Search.APIKey,
		Timeout:          time.Duration(cfg.Search.TimeoutSeconds) * time.Second,
		MaxResults:       cfg.Search.MaxResults,
		MaxResponseBytes: cfg.MaxResponseBytes,
		Policy:           webaccess.URLPolicy{AllowPrivateNetworks: cfg.AllowPrivateNetworks},
	})
	if err != nil {
		return toolsetWebServices{}, fmt.Errorf("web search service: %w", err)
	}
	return toolsetWebServices{fetch: fetch, search: search}, nil
}

func (f *RunnerFactory) buildBrowserService() (*tools.Service, error) {
	browserCfg := f.deps.Config.Browser
	webCfg := f.deps.Config.WebAccess
	return tools.NewService(tools.Config{
		ExecutablePath: strings.TrimSpace(browserCfg.ExecutablePath),
		Headless:       browserCfg.Headless,
		Timeout:        time.Duration(browserCfg.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      webCfg.UserAgent,
		Policy:         webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks},
	})
}

func (f *RunnerFactory) resolveOperatorStore() tools.OperatorQuestionStore {
	if f.deps.MCPPendingActions != nil {
		return f.deps.MCPPendingActions
	}
	return f.deps.Store
}

func (f *RunnerFactory) buildLocalCatalog(services toolsetWebServices, childExec orchestration.ChildAgentExecutor) (*tools.Catalog, []io.Closer, error) {
	browser, err := f.buildBrowserService()
	if err != nil {
		return nil, nil, fmt.Errorf("browser service: %w", err)
	}
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         f.deps.Workspace,
		MutationEnabled:   !f.deps.Config.Tools.Mutation.Disabled,
		RunCommandEnabled: !f.deps.Config.Tools.RunCommand.Disabled,
		ArtifactService:   f.deps.ArtifactService,
		ArtifactContext:   artifactToolBridge{},
		OperatorStore:     f.resolveOperatorStore(),
		OperatorContext:   artifactToolBridge{},
		WebFetchService:   services.fetch,
		WebSearchService:  services.search,
		BrowserService:    browser,
	}, f.deps.ExtraLocalTools, childExec, delegateTaskBridge{})
	return catalog, []io.Closer{browser}, err
}

func (f *RunnerFactory) buildAuxTools(ctx context.Context, sessionID string, includePlanning bool) (auxTools, error) {
	var out auxTools
	checkpoint, err := f.buildCheckpointTools(sessionID, includePlanning)
	if err != nil {
		return out, err
	}
	out.checkpoint = checkpoint
	memory, err := f.buildMemoryTools(ctx)
	if err != nil {
		return out, err
	}
	out.memory = memory
	skillTools, err := skills.BuildAgentTools(f.deps.Loader)
	if err != nil {
		return out, fmt.Errorf("build skill tools: %w", err)
	}
	out.skill = skillTools
	if includePlanning {
		lifecycle, err := f.buildSkillLifecycleTools()
		if err != nil {
			return out, err
		}
		out.lifecycle = lifecycle
	}
	return out, nil
}

func (f *RunnerFactory) buildCheckpointTools(sessionID string, includePlanning bool) ([]einotool.BaseTool, error) {
	checkpointService := f.deps.CheckpointService
	effectiveSessionID := sessionID
	if !includePlanning {
		checkpointService = nil
		effectiveSessionID = ""
	}
	tools, err := workingstate.BuildWorkingCheckpointTools(checkpointService, effectiveSessionID)
	if err != nil {
		return nil, fmt.Errorf("build working checkpoint tools: %w", err)
	}
	return tools, nil
}

func (f *RunnerFactory) buildMemoryTools(ctx context.Context) ([]einotool.BaseTool, error) {
	if f.deps.MemoryModule == nil {
		return nil, nil
	}
	return toolset.BuildMemoryFileTools(ctx, f.deps.MemoryModule, delegateTaskBridge{})
}

func (f *RunnerFactory) buildSkillLifecycleTools() ([]einotool.BaseTool, error) {
	lifecycle, err := skills.BuildSkillLifecycleTools(skills.ToolOptions{
		Loader: f.deps.Loader,
		Store:  f.deps.Store,
		Bridge: delegateTaskBridge{},
	})
	if err != nil {
		return nil, fmt.Errorf("build skill lifecycle tools: %w", err)
	}
	return lifecycle, nil
}
