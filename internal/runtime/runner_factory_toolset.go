package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/browser"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/skilllifecycle"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workingstate"
)

type Toolset struct {
	catalog *tooling.Catalog
	profile tooling.ToolProfile
	closers []toolsetCloser
}

type toolsetCloser interface {
	Close() error
}

func (t Toolset) All() []einotool.BaseTool {
	if t.catalog == nil {
		return nil
	}
	return t.catalog.ToolsForProfile(t.profile)
}

func (t Toolset) Catalog() *tooling.Catalog {
	return t.catalog
}

func (t *Toolset) Close() error {
	if t == nil {
		return nil
	}
	var errs []error
	for i := len(t.closers) - 1; i >= 0; i-- {
		if t.closers[i] == nil {
			continue
		}
		if err := t.closers[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f *RunnerFactory) BuildServeToolset(ctx context.Context) (*Toolset, error) {
	return f.buildToolset(ctx, "", nil, false, tooling.ToolProfileServe)
}

type delegateTaskBridge struct{}

func (delegateTaskBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (delegateTaskBridge) CurrentSessionID(ctx context.Context) string {
	return SessionIDFromContext(ctx)
}

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return SessionIDFromContext(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return toolAuditCallID(ctx)
}

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string, childExec orchestration.ChildAgentExecutor) (*Toolset, error) {
	return f.buildToolset(ctx, sessionID, childExec, true, tooling.ToolProfileRun)
}

func (f *RunnerFactory) buildToolset(
	ctx context.Context,
	sessionID string,
	childExec orchestration.ChildAgentExecutor,
	includePlanning bool,
	profile tooling.ToolProfile,
) (*Toolset, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.deps.Workspace == nil {
		return nil, errors.New("workspace contract is not initialized")
	}
	if f.deps.ArtifactService == nil {
		return nil, errors.New("artifact service is not initialized")
	}
	if f.deps.TerminalService == nil {
		return nil, errors.New("terminal session service is not initialized")
	}

	webFetchService, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        f.deps.Config.WebAccess.UserAgent,
		Timeout:          time.Duration(f.deps.Config.WebAccess.TimeoutSeconds) * time.Second,
		MaxResponseBytes: f.deps.Config.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web fetch service: %w", err)
	}
	webSearchService, err := webaccess.NewSearchService(webaccess.SearchConfig{
		APIKey:           f.deps.Config.WebAccess.Search.APIKey,
		Timeout:          time.Duration(f.deps.Config.WebAccess.Search.TimeoutSeconds) * time.Second,
		MaxResults:       f.deps.Config.WebAccess.Search.MaxResults,
		MaxResponseBytes: f.deps.Config.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web search service: %w", err)
	}
	browserService, err := browser.NewService(browser.Config{
		ExecutablePath: strings.TrimSpace(f.deps.Config.Browser.ExecutablePath),
		Headless:       f.deps.Config.Browser.Headless,
		Timeout:        time.Duration(f.deps.Config.Browser.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      f.deps.Config.WebAccess.UserAgent,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("browser service: %w", err)
	}

	var operatorStore tools.OperatorQuestionStore
	if f.deps.MCPPendingActions != nil {
		operatorStore = f.deps.MCPPendingActions
	} else if store, ok := f.deps.Store.(tools.OperatorQuestionStore); ok {
		operatorStore = store
	}

	localCatalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         f.deps.Workspace,
		MutationEnabled:   !f.deps.Config.Tools.Mutation.Disabled,
		RunCommandEnabled: !f.deps.Config.Tools.RunCommand.Disabled,
		ArtifactService:   f.deps.ArtifactService,
		ArtifactContext:   artifactToolBridge{},
		TerminalService:   f.deps.TerminalService,
		TerminalContext:   artifactToolBridge{},
		OperatorStore:     operatorStore,
		OperatorContext:   artifactToolBridge{},
		WebFetchService:   webFetchService,
		WebSearchService:  webSearchService,
		BrowserService:    browserService,
	}, f.deps.ExtraLocalTools, childExec, delegateTaskBridge{})
	if err != nil {
		return nil, err
	}

	checkpointService := f.deps.CheckpointService
	effectiveSessionID := sessionID
	if !includePlanning {
		checkpointService = nil
		effectiveSessionID = ""
	}
	checkpointTools, err := workingstate.BuildWorkingCheckpointTools(checkpointService, effectiveSessionID)
	if err != nil {
		return nil, fmt.Errorf("build working checkpoint tools: %w", err)
	}
	var memoryTools []einotool.BaseTool
	if f.deps.MemoryModule != nil {
		fileTools, err := buildMemoryFileTools(ctx, f.deps.MemoryModule)
		if err != nil {
			return nil, err
		}
		memoryTools = append(memoryTools, fileTools...)
	}

	skillTools, err := skills.BuildAgentTools(f.deps.Loader)
	if err != nil {
		return nil, fmt.Errorf("build skill tools: %w", err)
	}
	var skillLifecycleTools []einotool.BaseTool
	if includePlanning {
		skillLifecycleTools, err = skilllifecycle.BuildAgentTools(skilllifecycle.ToolOptions{
			Loader: f.deps.Loader,
			Store:  f.deps.Store,
			Bridge: delegateTaskBridge{},
		})
		if err != nil {
			return nil, fmt.Errorf("build skill lifecycle tools: %w", err)
		}
	}

	specs, err := buildCatalogSpecs(ctx, f.deps.Config, "local", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	checkpointSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "workingstate", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, checkpointTools)
	if err != nil {
		return nil, err
	}
	memorySpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "memory", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, memoryTools)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "skill", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, skillTools)
	if err != nil {
		return nil, err
	}
	skillLifecycleSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "skill.lifecycle", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun}, skillLifecycleTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, checkpointSpecs...)
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	specs = append(specs, skillLifecycleSpecs...)

	if includePlanning {
		loadToolsTool, err := newLoadToolsTool(f.deps.ContextPlane)
		if err != nil {
			return nil, fmt.Errorf("build load_tools tool: %w", err)
		}
		planningSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "runtime", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, []einotool.BaseTool{loadToolsTool})
		if err != nil {
			return nil, err
		}
		specs = append(specs, planningSpecs...)
	}
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return &Toolset{catalog: catalog, profile: profile, closers: []toolsetCloser{browserService}}, nil
}
