package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func buildToolset(ctx context.Context, deps RuntimeDeps) (*tools.Catalog, error) {
	if err := validateToolsetDeps(deps); err != nil {
		return nil, err
	}
	specs, err := buildLocalToolSpecs(deps)
	if err != nil {
		return nil, err
	}
	catalog, err := tools.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return catalog, nil
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

// buildLocalToolSpecs builds the run-scoped web and workstation tool specs from
// per-run services.
func buildLocalToolSpecs(deps RuntimeDeps) ([]core.ToolSpec, error) {
	webCfg, err := buildWebToolsConfig(deps)
	if err != nil {
		return nil, err
	}
	specs, err := tools.BuildWebToolSpecs(webCfg)
	if err != nil {
		return nil, err
	}
	workstationSpecs, err := tools.BuildWorkstationToolSpecs(deps.Workstation)
	if err != nil {
		return nil, err
	}
	return append(specs, workstationSpecs...), nil
}

// buildWebToolsConfig constructs the per-run web services. web_search stays
// disabled until its key is set; the reason surfaces in the capability
// snapshot.
func buildWebToolsConfig(deps RuntimeDeps) (tools.WebToolsConfig, error) {
	webCfg := deps.Config.WebAccess
	policy := webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks}
	fetch, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        webaccess.UserAgent,
		Timeout:          webaccess.FetchTimeout,
		MaxResponseBytes: webaccess.MaxResponseBytes,
		Policy:           policy,
	})
	if err != nil {
		return tools.WebToolsConfig{}, fmt.Errorf("web fetch service: %w", err)
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
			Timeout:          webaccess.SearchTimeout,
			MaxResults:       webaccess.SearchMaxResults,
			MaxResponseBytes: webaccess.MaxResponseBytes,
			Policy:           policy,
		})
		if err != nil {
			return tools.WebToolsConfig{}, fmt.Errorf("web search service: %w", err)
		}
		out.Search = search
	}
	return out, nil
}

// buildRunCapabilities builds the run's tool catalog (local tools + MCP specs)
// and resolves a stable skill snapshot for capability eligibility. The tools
// of the skills' own MCP servers join the catalog once the servers serve.
func buildRunCapabilities(ctx context.Context, deps RuntimeDeps, sessionID, runID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	toolset, err := buildToolset(ctx, deps)
	if err != nil {
		return nil, err
	}
	catalog, err := assembleRunCapabilitiesCatalog(ctx, deps, toolset, sessionID, runID, mcpManager)
	if err != nil {
		return nil, err
	}
	skillSnapshot, err := loadStableSkillSnapshot(ctx, deps.Skills, tools.EligibilityContext(catalog))
	if err != nil {
		return nil, err
	}
	skillDir, synced := syncSkillPackages(ctx, deps.Skills, deps.SkillPackages, *skillSnapshot)
	served, skillTools := serveSkillTools(ctx, deps, synced)
	if len(skillTools) > 0 {
		if catalog, err = tools.NewCatalog(ctx, append(catalog.Specs(), skillTools...)); err != nil {
			return nil, fmt.Errorf("add skill tools: %w", err)
		}
	}
	return &runCapabilities{
		catalog:       catalog,
		skillSnapshot: &served,
		skillDir:      skillDir,
	}, nil
}

// assembleRunCapabilitiesCatalog builds the final run capability catalog by
// merging three sources:
//   - registry specs: static native tools + MCP main tools (MCP tools
//     are registered into the registry at provider-connect time)
//   - toolset catalog specs: run-scoped native tools (web, workstation,
//     built per run from live services)
//   - MCP auxiliary specs: resource/prompt wrappers (session-derived, outside
//     the registry lifecycle)
//
// There is no overlap between registry and toolset specs: the registry owns
// static natives, the toolset owns run-scoped natives.
func assembleRunCapabilitiesCatalog(ctx context.Context, deps RuntimeDeps, toolset *tools.Catalog, sessionID, runID string, mcpManager *mcpprovider.Manager) (*tools.Catalog, error) {
	registrySpecs, err := deps.ToolRegistry.ResolveEnabledSpecs(ctx, core.RunContext{RunID: runID, SessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("resolve registry tools: %w", err)
	}
	specs := append([]core.ToolSpec(nil), registrySpecs...)
	specs = append(specs, toolset.Specs()...)
	mcpSpecs, err := buildMCPAuxiliaryToolSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	return tools.NewCatalog(ctx, specs)
}
