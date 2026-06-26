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
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/tools/dispatch"
	"github.com/ycvk/acorn/internal/webaccess"
)

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return core.CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return core.GetSessionID(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return dispatch.ToolAuditCallID(ctx)
}

// NewContextBridge returns a ToolCallContextBridge that reads run/session/tool-call
// identifiers from the context. Used by wire to pass context plumbing into
// RegisterNativeTools so tool factories have the bridge at resolve time.
func NewContextBridge() core.ToolCallContextBridge {
	return artifactToolBridge{}
}

func buildToolset(
	ctx context.Context,
	deps RuntimeDeps,
	sessionID string,
) (_ *Toolset, err error) {
	if err := validateToolsetDeps(deps); err != nil {
		return nil, err
	}
	var closers []io.Closer
	defer func() { closeToolsetOnErr(closers, &err) }()
	local, err := buildLocalToolset(ctx, deps)
	if err != nil {
		return nil, err
	}
	closers = append(closers, local.closers...)
	aux, err := buildAuxTools(ctx, deps)
	if err != nil {
		return nil, err
	}
	catalog, err := assembleToolsetCatalog(ctx, deps.Config, local.catalog, aux)
	if err != nil {
		return nil, err
	}
	return NewToolset(catalog, closers...), nil
}

func validateToolsetDeps(deps RuntimeDeps) error {
	if deps.Config == nil {
		return errors.New("runner factory is not initialized")
	}
	if deps.Workspace == nil {
		return errors.New("workspace contract is not initialized")
	}
	if deps.ArtifactService == nil {
		return errors.New("artifact service is not initialized")
	}
	return nil
}

func buildLocalToolset(ctx context.Context, deps RuntimeDeps) (localToolset, error) {
	var out localToolset
	services, err := buildToolsetWebServices(deps)
	if err != nil {
		return out, err
	}
	out.catalog, out.closers, err = buildLocalCatalog(ctx, deps, services)
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

func assembleToolsetCatalog(ctx context.Context, cfg *config.Config, localCatalog *tools.LocalCatalog, aux auxTools) (*tools.Catalog, error) {
	coreSpecs, err := buildCoreToolSpecs(ctx, cfg, localCatalog, aux)
	if err != nil {
		return nil, err
	}
	extra, err := buildExtraToolSpecs(ctx, cfg, aux)
	if err != nil {
		return nil, err
	}
	specs := append(coreSpecs, extra...)
	catalog, err := tools.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return catalog, nil
}

// buildCoreToolSpecs builds the specs the toolset catalog owns: deferred-loaded
// native tools (web_fetch, web_search, browser — which depend on per-run web
// services) plus memory and skill tools. Eager-loaded native tools are owned by
// the registry and are not built here.
func buildCoreToolSpecs(ctx context.Context, cfg *config.Config, localCatalog *tools.LocalCatalog, aux auxTools) ([]core.ToolSpec, error) {
	var specs []core.ToolSpec
	for _, tool := range localCatalog.Tools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info for local toolset: %w", err)
		}
		name := strings.TrimSpace(info.Name)
		// Only deferred-loaded tools belong to the toolset catalog; eager
		// natives are owned by the registry.
		localSpec, ok := tools.ConfiguredLocalSpec(cfg, name)
		if !ok {
			continue
		}
		if localSpec.Loading.Mode != core.ToolLoadingModeDeferred {
			continue
		}
		localSpec.Tool = tool
		specs = append(specs, localSpec)
	}
	memorySpecs, err := BuildCatalogSpecs(ctx, cfg, "memory", core.ToolKindMemory, aux.memory)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := BuildCatalogSpecs(ctx, cfg, "skill", core.ToolKindSkill, aux.skill)
	if err != nil {
		return nil, err
	}
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	return specs, nil
}

func buildExtraToolSpecs(ctx context.Context, cfg *config.Config, aux auxTools) ([]core.ToolSpec, error) {
	loadToolsTool, err := NewLoadToolsTool()
	if err != nil {
		return nil, fmt.Errorf("build load_tools tool: %w", err)
	}
	planningSpecs, err := BuildCatalogSpecs(ctx, cfg, "runtime", core.ToolKindNative, []einotool.BaseTool{loadToolsTool})
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

func buildToolsetWebServices(deps RuntimeDeps) (toolsetWebServices, error) {
	cfg := deps.Config.WebAccess
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

func buildBrowserService(deps RuntimeDeps) (*tools.Service, error) {
	browserCfg := deps.Config.Browser
	webCfg := deps.Config.WebAccess
	return tools.NewService(tools.Config{
		ExecutablePath: strings.TrimSpace(browserCfg.ExecutablePath),
		Headless:       browserCfg.Headless,
		Timeout:        time.Duration(browserCfg.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      webCfg.UserAgent,
		Policy:         webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks},
	})
}

func resolveOperatorStore(deps RuntimeDeps) tools.OperatorQuestionStore {
	if deps.MCPPendingActions != nil {
		return deps.MCPPendingActions
	}
	return deps.Store
}

func buildLocalCatalog(ctx context.Context, deps RuntimeDeps, services toolsetWebServices) (*tools.LocalCatalog, []io.Closer, error) {
	browser, err := buildBrowserService(deps)
	if err != nil {
		return nil, nil, fmt.Errorf("browser service: %w", err)
	}
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         deps.Workspace,
		MutationEnabled:   !deps.Config.Tools.Mutation.Disabled,
		RunCommandEnabled: !deps.Config.Tools.RunCommand.Disabled,
		ArtifactService:   deps.ArtifactService,
		ArtifactContext:   artifactToolBridge{},
		OperatorStore:     resolveOperatorStore(deps),
		OperatorContext:   artifactToolBridge{},
		WebFetchService:   services.fetch,
		WebSearchService:  services.search,
		BrowserService:    browser,
	}, deps.ExtraLocalTools)
	return catalog, []io.Closer{browser}, err
}

func buildAuxTools(ctx context.Context, deps RuntimeDeps) (auxTools, error) {
	var out auxTools
	memory, err := buildMemoryTools(ctx, deps)
	if err != nil {
		return out, err
	}
	out.memory = memory
	skillTools, err := skills.BuildAgentTools(deps.Loader)
	if err != nil {
		return out, fmt.Errorf("build skill tools: %w", err)
	}
	out.skill = skillTools
	return out, nil
}

func buildMemoryTools(ctx context.Context, deps RuntimeDeps) ([]einotool.BaseTool, error) {
	if deps.MemoryModule == nil {
		return nil, nil
	}
	return BuildMemoryFileTools(ctx, deps.MemoryModule)
}

// buildRunCapabilities builds the run's tool catalog (local tools + MCP specs)
// and resolves a stable skill snapshot for capability eligibility.
func buildRunCapabilities(ctx context.Context, deps RuntimeDeps, sessionID, runID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	toolset, err := buildToolset(ctx, deps, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = toolset.Close()
		}
	}()
	catalog, err := assembleRunCapabilitiesCatalog(ctx, deps, toolset, sessionID, runID, mcpManager)
	if err != nil {
		return nil, err
	}
	skillSnapshot, err := loadStableSkillSnapshot(ctx, deps.Loader, skillEligibilityContextFromCatalog(catalog))
	if err != nil {
		return nil, err
	}
	return &runCapabilities{
		catalog:       catalog,
		skillSnapshot: skillSnapshot,
		stableSkills:  stableSkillsFromSnapshot(skillSnapshot),
		close:         toolset.Close,
	}, nil
}

// assembleRunCapabilitiesCatalog builds the final run capability catalog by
// merging three sources:
//   - registry specs: eager-loaded native tools + MCP main tools (MCP tools
//     are registered into the registry at provider-connect time)
//   - toolset catalog specs: deferred-loaded native tools (web/browser, built
//     per run from live services), memory, skill, and load_tools
//   - MCP auxiliary specs: resource/prompt wrappers (session-derived, outside
//     the registry lifecycle)
//
// There is no overlap between registry and toolset specs: the registry owns
// eager natives, the toolset owns deferred natives + non-native tools.
func assembleRunCapabilitiesCatalog(ctx context.Context, deps RuntimeDeps, toolset *Toolset, sessionID, runID string, mcpManager *mcpprovider.Manager) (*tools.Catalog, error) {
	registrySpecs, err := resolveRegistrySpecs(ctx, deps, sessionID, runID)
	if err != nil {
		return nil, fmt.Errorf("resolve registry tools: %w", err)
	}
	specs := append([]core.ToolSpec(nil), registrySpecs...)
	specs = append(specs, toolset.Catalog().Specs()...)
	mcpSpecs, err := buildMCPAuxiliaryToolSpecs(ctx, deps.Config, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	return tools.NewCatalog(ctx, specs)
}

// resolveRegistrySpecs resolves every enabled tool spec from the registry into
// a concrete tool instance under the given run context, returning specs with
// the Tool field populated so the audited-tool builder can use them directly.
func resolveRegistrySpecs(ctx context.Context, deps RuntimeDeps, sessionID, runID string) ([]core.ToolSpec, error) {
	runCtx := core.RunContext{RunID: runID, SessionID: sessionID}
	return deps.ToolRegistry.ResolveEnabledSpecs(ctx, runCtx)
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
