package toolfactory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"

	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workingstate"
)

// Builder constructs tool catalogs for runs and serve mode.
type Builder struct {
	cfg               *config.Config
	workspace         tools.WorkspaceView
	artifactService   tools.ArtifactService
	checkpointService *workingstate.Service
	memoryModule      memorymodule.Service
	loader            skills.SkillLoader
	store             skills.LifecycleEventAppender
	mcpPendingActions mcpprovider.PendingActionStore
	extraLocalTools   []einotool.BaseTool
	contextPlane      contextplane.ContextPlane
}

// NewBuilder creates a tool builder from dependencies.
func NewBuilder(
	cfg *config.Config,
	workspace tools.WorkspaceView,
	artifactService tools.ArtifactService,
	checkpointService *workingstate.Service,
	memoryModule memorymodule.Service,
	loader skills.SkillLoader,
	store skills.LifecycleEventAppender,
	mcpPendingActions mcpprovider.PendingActionStore,
	extraLocalTools []einotool.BaseTool,
	contextPlane contextplane.ContextPlane,
) *Builder {
	return &Builder{
		cfg:               cfg,
		workspace:         workspace,
		artifactService:   artifactService,
		checkpointService: checkpointService,
		memoryModule:      memoryModule,
		loader:            loader,
		store:             store,
		mcpPendingActions: mcpPendingActions,
		extraLocalTools:   append([]einotool.BaseTool(nil), extraLocalTools...),
		contextPlane:      contextPlane,
	}
}

// BuildOptions are the options for a single toolset build.
type BuildOptions struct {
	SessionID           string
	ChildExecutor       orchestration.ChildAgentExecutor
	IncludePlanning     bool
	Profile             tooling.ToolProfile
	ArtifactContext     tools.ArtifactContext
	OperatorContext     tools.OperatorQuestionContext
	DelegateContext     tools.DelegateTaskContext
	RunContextBridge    skills.RunContextBridge
	RunContextExtractor RunContextExtractor
}

// Build constructs a Toolset for the given options.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*Toolset, error) {
	if b == nil || b.cfg == nil {
		return nil, errors.New("tool builder is not initialized")
	}
	if b.workspace == nil {
		return nil, errors.New("workspace is required for tool building")
	}

	webFetchService, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        b.cfg.WebAccess.UserAgent,
		Timeout:          time.Duration(b.cfg.WebAccess.TimeoutSeconds) * time.Second,
		MaxResponseBytes: b.cfg.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: b.cfg.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web fetch service: %w", err)
	}
	webSearchService, err := webaccess.NewSearchService(webaccess.SearchConfig{
		APIKey:           b.cfg.WebAccess.Search.APIKey,
		Timeout:          time.Duration(b.cfg.WebAccess.Search.TimeoutSeconds) * time.Second,
		MaxResults:       b.cfg.WebAccess.Search.MaxResults,
		MaxResponseBytes: b.cfg.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: b.cfg.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web search service: %w", err)
	}
	browserService, err := tools.NewService(tools.Config{
		ExecutablePath: strings.TrimSpace(b.cfg.Browser.ExecutablePath),
		Headless:       b.cfg.Browser.Headless,
		Timeout:        time.Duration(b.cfg.Browser.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      b.cfg.WebAccess.UserAgent,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: b.cfg.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("browser service: %w", err)
	}

	var operatorStore tools.OperatorQuestionStore
	if b.mcpPendingActions != nil {
		operatorStore = b.mcpPendingActions
	} else if store, ok := b.store.(tools.OperatorQuestionStore); ok {
		operatorStore = store
	}

	localCatalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         b.workspace,
		MutationEnabled:   !b.cfg.Tools.Mutation.Disabled,
		RunCommandEnabled: !b.cfg.Tools.RunCommand.Disabled,
		ArtifactService:   b.artifactService,
		ArtifactContext:   opts.ArtifactContext,
		OperatorStore:     operatorStore,
		OperatorContext:   opts.OperatorContext,
		WebFetchService:   webFetchService,
		WebSearchService:  webSearchService,
		BrowserService:    browserService,
	}, b.extraLocalTools, opts.ChildExecutor, opts.DelegateContext)
	if err != nil {
		return nil, err
	}

	checkpointService := b.checkpointService
	effectiveSessionID := opts.SessionID
	if !opts.IncludePlanning {
		checkpointService = nil
		effectiveSessionID = ""
	}
	checkpointTools, err := workingstate.BuildWorkingCheckpointTools(checkpointService, effectiveSessionID)
	if err != nil {
		return nil, fmt.Errorf("build working checkpoint tools: %w", err)
	}
	var memoryTools []einotool.BaseTool
	if b.memoryModule != nil {
		fileTools, err := BuildMemoryFileTools(ctx, b.memoryModule, nil)
		if err != nil {
			return nil, err
		}
		memoryTools = append(memoryTools, fileTools...)
	}

	skillTools, err := skills.BuildAgentTools(b.loader)
	if err != nil {
		return nil, fmt.Errorf("build skill tools: %w", err)
	}
	var skillLifecycleTools []einotool.BaseTool
	if opts.IncludePlanning {
		skillLifecycleTools, err = skills.BuildSkillLifecycleTools(skills.ToolOptions{
			Loader: b.loader,
			Store:  b.store,
			Bridge: opts.RunContextBridge,
		})
		if err != nil {
			return nil, fmt.Errorf("build skill lifecycle tools: %w", err)
		}
	}

	specs, err := BuildCatalogSpecs(ctx, b.cfg, "local", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	checkpointSpecs, err := BuildCatalogSpecs(ctx, b.cfg, "workingstate", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, checkpointTools)
	if err != nil {
		return nil, err
	}
	memorySpecs, err := BuildCatalogSpecs(ctx, b.cfg, "memory", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, memoryTools)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := BuildCatalogSpecs(ctx, b.cfg, "skill", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, skillTools)
	if err != nil {
		return nil, err
	}
	skillLifecycleSpecs, err := BuildCatalogSpecs(ctx, b.cfg, "skill.lifecycle", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun}, skillLifecycleTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, checkpointSpecs...)
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	specs = append(specs, skillLifecycleSpecs...)

	if opts.IncludePlanning {
		loadToolsTool, err := NewLoadToolsTool(opts.RunContextExtractor)
		if err != nil {
			return nil, fmt.Errorf("build load_tools tool: %w", err)
		}
		planningSpecs, err := BuildCatalogSpecs(ctx, b.cfg, "runtime", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, []einotool.BaseTool{loadToolsTool})
		if err != nil {
			return nil, err
		}
		specs = append(specs, planningSpecs...)
	}
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return NewToolset(catalog, opts.Profile, browserService), nil
}
