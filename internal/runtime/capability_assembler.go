package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/tools"
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
	return compose.GetToolCallID(ctx)
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
	local, err := buildLocalToolset(deps)
	closers = append(closers, local.closers...)
	if err != nil {
		return nil, err
	}
	catalog, err := tools.NewCatalog(ctx, local.specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return NewToolset(catalog, closers...), nil
}

func validateToolsetDeps(deps RuntimeDeps) error {
	if deps.Config == nil {
		return errors.New("runner factory is not initialized")
	}
	if deps.ArtifactService == nil {
		return errors.New("artifact service is not initialized")
	}
	return nil
}

// buildLocalToolset builds the deferred web tool specs from per-run services.
// The returned closers must be closed even when err is non-nil.
func buildLocalToolset(deps RuntimeDeps) (localToolset, error) {
	webCfg, closers, err := buildWebToolsConfig(deps)
	if err != nil {
		return localToolset{}, err
	}
	specs, err := tools.BuildWebToolSpecs(webCfg)
	return localToolset{specs: specs, closers: closers}, err
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

// buildWebToolsConfig constructs the per-run web services. web_search and
// browser stay disabled until their config is set; the reason surfaces in the
// capability snapshot.
func buildWebToolsConfig(deps RuntimeDeps) (tools.WebToolsConfig, []io.Closer, error) {
	webCfg := deps.Config.WebAccess
	policy := webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks}
	fetch, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        webCfg.UserAgent,
		Timeout:          time.Duration(webCfg.TimeoutSeconds) * time.Second,
		MaxResponseBytes: webCfg.MaxResponseBytes,
		Policy:           policy,
	})
	if err != nil {
		return tools.WebToolsConfig{}, nil, fmt.Errorf("web fetch service: %w", err)
	}
	out := tools.WebToolsConfig{
		ArtifactService: deps.ArtifactService,
		ArtifactContext: artifactToolBridge{},
		Fetch:           fetch,
	}
	if strings.TrimSpace(webCfg.Search.APIKey) == "" {
		out.SearchDisabledReason = "web_access.search.api_key is not configured"
	} else {
		search, err := webaccess.NewSearchService(webaccess.SearchConfig{
			APIKey:           webCfg.Search.APIKey,
			Timeout:          time.Duration(webCfg.Search.TimeoutSeconds) * time.Second,
			MaxResults:       webCfg.Search.MaxResults,
			MaxResponseBytes: webCfg.MaxResponseBytes,
			Policy:           policy,
		})
		if err != nil {
			return tools.WebToolsConfig{}, nil, fmt.Errorf("web search service: %w", err)
		}
		out.Search = search
	}
	if strings.TrimSpace(deps.Config.Browser.ExecutablePath) == "" {
		out.BrowserDisabledReason = "browser.executable_path is not configured"
		return out, nil, nil
	}
	browser, err := buildBrowserService(deps)
	if err != nil {
		return tools.WebToolsConfig{}, nil, fmt.Errorf("browser service: %w", err)
	}
	out.Browser = browser
	return out, []io.Closer{browser}, nil
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
		close:         toolset.Close,
	}, nil
}

// assembleRunCapabilitiesCatalog builds the final run capability catalog by
// merging three sources:
//   - registry specs: eager-loaded native tools + MCP main tools (MCP tools
//     are registered into the registry at provider-connect time)
//   - toolset catalog specs: deferred-loaded native tools (web/browser, built
//     per run from live services)
//   - MCP auxiliary specs: resource/prompt wrappers (session-derived, outside
//     the registry lifecycle)
//
// There is no overlap between registry and toolset specs: the registry owns
// eager natives, the toolset owns deferred natives.
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
