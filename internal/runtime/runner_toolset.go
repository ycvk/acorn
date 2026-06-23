package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/factextract"
	"github.com/ycvk/acorn/internal/runtime/tooldispatch"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/toolset"
	"github.com/ycvk/acorn/internal/webaccess"
)

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return domain.SessionIDFromContext(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return tooldispatch.ToolAuditCallID(ctx)
}

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string) (*Toolset, error) {
	return f.buildToolset(ctx, sessionID, true)
}

func (f *RunnerFactory) buildToolset(
	ctx context.Context,
	sessionID string,
	includePlanning bool,
) (_ *Toolset, err error) {
	if err := f.validateToolsetDeps(); err != nil {
		return nil, err
	}
	var closers []io.Closer
	defer func() { closeToolsetOnErr(closers, &err) }()
	local, err := f.buildLocalToolset()
	if err != nil {
		return nil, err
	}
	closers = append(closers, local.closers...)
	aux, err := f.buildAuxTools(ctx)
	if err != nil {
		return nil, err
	}
	catalog, err := assembleToolsetCatalog(ctx, f.deps.Config, local.catalog, aux, includePlanning)
	if err != nil {
		return nil, err
	}
	return NewToolset(catalog, closers...), nil
}

func (f *RunnerFactory) validateToolsetDeps() error {
	if f == nil || f.deps.Config == nil {
		return errors.New("runner factory is not initialized")
	}
	if f.deps.Workspace == nil {
		return errors.New("workspace contract is not initialized")
	}
	if f.deps.ArtifactService == nil {
		return errors.New("artifact service is not initialized")
	}
	return nil
}

func (f *RunnerFactory) buildLocalToolset() (localToolset, error) {
	var out localToolset
	services, err := f.buildToolsetWebServices()
	if err != nil {
		return out, err
	}
	out.catalog, out.closers, err = f.buildLocalCatalog(services)
	return out, err
}

func closeToolsetOnErr(closers []io.Closer, err *error) {
	if *err == nil {
		return
	}
	var closeErrs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if closers[i] == nil {
			continue
		}
		if closeErr := closers[i].Close(); closeErr != nil {
			closeErrs = append(closeErrs, closeErr)
		}
	}
	if len(closeErrs) > 0 {
		*err = errors.Join(*err, fmt.Errorf("close toolset after build failure: %w", errors.Join(closeErrs...)))
	}
}

func assembleToolsetCatalog(ctx context.Context, cfg *config.Config, localCatalog *toolset.Catalog, aux auxTools, includePlanning bool) (*tools.Catalog, error) {
	core, err := buildCoreToolSpecs(ctx, cfg, localCatalog, aux)
	if err != nil {
		return nil, err
	}
	extra, err := buildExtraToolSpecs(ctx, cfg, aux, includePlanning)
	if err != nil {
		return nil, err
	}
	specs := append(core, extra...)
	catalog, err := tools.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return catalog, nil
}

func buildCoreToolSpecs(ctx context.Context, cfg *config.Config, localCatalog *toolset.Catalog, aux auxTools) ([]tools.ToolSpec, error) {
	specs, err := BuildCatalogSpecs(ctx, cfg, "local", tools.ToolKindNative, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	memorySpecs, err := BuildCatalogSpecs(ctx, cfg, "memory", tools.ToolKindMemory, aux.memory)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := BuildCatalogSpecs(ctx, cfg, "skill", tools.ToolKindSkill, aux.skill)
	if err != nil {
		return nil, err
	}
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	return specs, nil
}

func buildExtraToolSpecs(ctx context.Context, cfg *config.Config, aux auxTools, includePlanning bool) ([]tools.ToolSpec, error) {
	if !includePlanning {
		return nil, nil
	}
	loadToolsTool, err := NewLoadToolsTool()
	if err != nil {
		return nil, fmt.Errorf("build load_tools tool: %w", err)
	}
	planningSpecs, err := BuildCatalogSpecs(ctx, cfg, "runtime", tools.ToolKindNative, []einotool.BaseTool{loadToolsTool})
	if err != nil {
		return nil, err
	}
	return planningSpecs, nil
}

type toolsetWebServices struct {
	fetch  *webaccess.FetchService
	search *webaccess.SearchService
}

type auxTools struct {
	memory []einotool.BaseTool
	skill  []einotool.BaseTool
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

func (f *RunnerFactory) buildBrowserService() (*toolset.Service, error) {
	browserCfg := f.deps.Config.Browser
	webCfg := f.deps.Config.WebAccess
	return toolset.NewService(toolset.Config{
		ExecutablePath: strings.TrimSpace(browserCfg.ExecutablePath),
		Headless:       browserCfg.Headless,
		Timeout:        time.Duration(browserCfg.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      webCfg.UserAgent,
		Policy:         webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks},
	})
}

func (f *RunnerFactory) resolveOperatorStore() toolset.OperatorQuestionStore {
	if f.deps.MCPPendingActions != nil {
		return f.deps.MCPPendingActions
	}
	return f.deps.Store
}

func (f *RunnerFactory) buildLocalCatalog(services toolsetWebServices) (*toolset.Catalog, []io.Closer, error) {
	browser, err := f.buildBrowserService()
	if err != nil {
		return nil, nil, fmt.Errorf("browser service: %w", err)
	}
	catalog, err := toolset.BuildCatalog(toolset.CatalogConfig{
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
	}, f.deps.ExtraLocalTools)
	return catalog, []io.Closer{browser}, err
}

func (f *RunnerFactory) buildAuxTools(ctx context.Context) (auxTools, error) {
	var out auxTools
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
	return out, nil
}

func (f *RunnerFactory) buildMemoryTools(ctx context.Context) ([]einotool.BaseTool, error) {
	if f.deps.MemoryModule == nil {
		return nil, nil
	}
	return factextract.BuildMemoryFileTools(ctx, f.deps.MemoryModule)
}

func NewToolset(catalog *tools.Catalog, closers ...io.Closer) *Toolset {
	c := make([]io.Closer, 0, len(closers))
	for _, cl := range closers {
		if cl != nil {
			c = append(c, cl)
		}
	}
	return &Toolset{catalog: catalog, closers: c}
}

// Toolset is a built collection of tools for a run or serve context.
type Toolset struct {
	catalog *tools.Catalog
	closers []io.Closer
}

func (t Toolset) All() []einotool.BaseTool {
	if t.catalog == nil {
		return nil
	}
	return t.catalog.Tools()
}

func (t Toolset) Catalog() *tools.Catalog {
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
