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
	"github.com/ycvk/acorn/internal/webaccess"
)

// CapabilityAssembler owns toolset/capability construction for a run. It
// isolates the local tool catalog, aux tool, and MCP-augmented capability
// assembly from the RunnerFactory so the factory stays a thin coordinator.
type CapabilityAssembler struct {
	deps RuntimeDeps
}

// NewCapabilityAssembler assembles a CapabilityAssembler from runtime deps.
func NewCapabilityAssembler(deps RuntimeDeps) *CapabilityAssembler {
	return &CapabilityAssembler{deps: deps}
}

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return core.GetSessionID(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return tools.ToolAuditCallID(ctx)
}

func (a *CapabilityAssembler) buildRunToolset(ctx context.Context, sessionID string) (*Toolset, error) {
	return a.buildToolset(ctx, sessionID, true)
}

func (a *CapabilityAssembler) buildToolset(
	ctx context.Context,
	sessionID string,
	includePlanning bool,
) (_ *Toolset, err error) {
	if err := a.validateToolsetDeps(); err != nil {
		return nil, err
	}
	var closers []io.Closer
	defer func() { closeToolsetOnErr(closers, &err) }()
	local, err := a.buildLocalToolset()
	if err != nil {
		return nil, err
	}
	closers = append(closers, local.closers...)
	aux, err := a.buildAuxTools(ctx)
	if err != nil {
		return nil, err
	}
	catalog, err := assembleToolsetCatalog(ctx, a.deps.Config, local.catalog, aux, includePlanning)
	if err != nil {
		return nil, err
	}
	return NewToolset(catalog, closers...), nil
}

func (a *CapabilityAssembler) validateToolsetDeps() error {
	if a == nil || a.deps.Config == nil {
		return errors.New("runner factory is not initialized")
	}
	if a.deps.Workspace == nil {
		return errors.New("workspace contract is not initialized")
	}
	if a.deps.ArtifactService == nil {
		return errors.New("artifact service is not initialized")
	}
	return nil
}

func (a *CapabilityAssembler) buildLocalToolset() (localToolset, error) {
	var out localToolset
	services, err := a.buildToolsetWebServices()
	if err != nil {
		return out, err
	}
	out.catalog, out.closers, err = a.buildLocalCatalog(services)
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

func assembleToolsetCatalog(ctx context.Context, cfg *config.Config, localCatalog *tools.LocalCatalog, aux auxTools, includePlanning bool) (*tools.Catalog, error) {
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

func buildCoreToolSpecs(ctx context.Context, cfg *config.Config, localCatalog *tools.LocalCatalog, aux auxTools) ([]core.ToolSpec, error) {
	specs, err := BuildCatalogSpecs(ctx, cfg, "local", core.ToolKindNative, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
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

func buildExtraToolSpecs(ctx context.Context, cfg *config.Config, aux auxTools, includePlanning bool) ([]core.ToolSpec, error) {
	if !includePlanning {
		return nil, nil
	}
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

func (a *CapabilityAssembler) buildToolsetWebServices() (toolsetWebServices, error) {
	cfg := a.deps.Config.WebAccess
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

func (a *CapabilityAssembler) buildBrowserService() (*tools.Service, error) {
	browserCfg := a.deps.Config.Browser
	webCfg := a.deps.Config.WebAccess
	return tools.NewService(tools.Config{
		ExecutablePath: strings.TrimSpace(browserCfg.ExecutablePath),
		Headless:       browserCfg.Headless,
		Timeout:        time.Duration(browserCfg.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      webCfg.UserAgent,
		Policy:         webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks},
	})
}

func (a *CapabilityAssembler) resolveOperatorStore() tools.OperatorQuestionStore {
	if a.deps.MCPPendingActions != nil {
		return a.deps.MCPPendingActions
	}
	return a.deps.Store
}

func (a *CapabilityAssembler) buildLocalCatalog(services toolsetWebServices) (*tools.LocalCatalog, []io.Closer, error) {
	browser, err := a.buildBrowserService()
	if err != nil {
		return nil, nil, fmt.Errorf("browser service: %w", err)
	}
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         a.deps.Workspace,
		MutationEnabled:   !a.deps.Config.Tools.Mutation.Disabled,
		RunCommandEnabled: !a.deps.Config.Tools.RunCommand.Disabled,
		ArtifactService:   a.deps.ArtifactService,
		ArtifactContext:   artifactToolBridge{},
		OperatorStore:     a.resolveOperatorStore(),
		OperatorContext:   artifactToolBridge{},
		WebFetchService:   services.fetch,
		WebSearchService:  services.search,
		BrowserService:    browser,
	}, a.deps.ExtraLocalTools)
	return catalog, []io.Closer{browser}, err
}

func (a *CapabilityAssembler) buildAuxTools(ctx context.Context) (auxTools, error) {
	var out auxTools
	memory, err := a.buildMemoryTools(ctx)
	if err != nil {
		return out, err
	}
	out.memory = memory
	skillTools, err := skills.BuildAgentTools(a.deps.Loader)
	if err != nil {
		return out, fmt.Errorf("build skill tools: %w", err)
	}
	out.skill = skillTools
	return out, nil
}

func (a *CapabilityAssembler) buildMemoryTools(ctx context.Context) ([]einotool.BaseTool, error) {
	if a.deps.MemoryModule == nil {
		return nil, nil
	}
	return BuildMemoryFileTools(ctx, a.deps.MemoryModule)
}

// buildRunCapabilities builds the run's tool catalog (local tools + MCP specs)
// and resolves a stable skill snapshot for capability eligibility.
func (a *CapabilityAssembler) buildRunCapabilities(ctx context.Context, sessionID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	toolset, err := a.buildRunToolset(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = toolset.Close()
		}
	}()
	catalog, err := a.assembleRunCapabilitiesCatalog(ctx, toolset, mcpManager)
	if err != nil {
		return nil, err
	}
	skillSnapshot, err := loadStableSkillSnapshot(ctx, a.deps.Loader, skillEligibilityContextFromCatalog(catalog))
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

// assembleRunCapabilitiesCatalog merges the local toolset catalog with MCP tool
// specs into the final run capability catalog.
func (a *CapabilityAssembler) assembleRunCapabilitiesCatalog(ctx context.Context, toolset *Toolset, mcpManager *mcpprovider.Manager) (*tools.Catalog, error) {
	specs := append([]core.ToolSpec(nil), toolset.Catalog().Specs()...)
	mcpSpecs, err := buildMCPToolSpecs(ctx, a.deps.Config, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	return tools.NewCatalog(ctx, specs)
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
