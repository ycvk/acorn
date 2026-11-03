package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
)

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return core.CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return core.CurrentSessionID(ctx)
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

// buildRunCapabilities builds the run's tool catalog (local tools + MCP specs
// + the run's own tools) and resolves a stable skill snapshot for capability
// eligibility. The tools of the skills' own MCP servers join the catalog once
// the servers serve. A non-nil keep leaves out the tools it does not name,
// then the skills that need them, with their servers' tools.
func buildRunCapabilities(ctx context.Context, deps RuntimeDeps, mcpManager *mcpprovider.Manager, own []core.ToolSpec, keep func(name string) bool) (*runCapabilities, error) {
	toolset, err := buildToolset(ctx, deps)
	if err != nil {
		return nil, err
	}
	base, err := assembleRunCapabilitiesCatalog(ctx, deps, toolset, mcpManager)
	if err != nil {
		return nil, err
	}
	// The workstation holds the packages and runs the servers of what a run
	// of the conversation can use, delegate included, whichever run syncs
	// them; a sub-run keeping fewer tools leaves them in place.
	eligibility := tools.EligibilityContext(base)
	eligibility.AvailableTools = append(eligibility.AvailableTools, "delegate")
	skillSnapshot, err := loadStableSkillSnapshot(ctx, deps.Skills, eligibility)
	if err != nil {
		return nil, err
	}
	skillDir, synced := syncSkillPackages(ctx, deps.Skills, deps.SkillPackages, *skillSnapshot)
	served, skillTools := serveSkillTools(ctx, deps, synced)
	specs := keptSpecs(append(base.Specs(), own...), keep)
	if keep != nil {
		kept, err := tools.NewCatalog(ctx, specs)
		if err != nil {
			return nil, fmt.Errorf("keep the run's tools: %w", err)
		}
		served = narrowSkills(served, tools.EligibilityContext(kept))
		skillTools = toolsOfEligibleSkills(skillTools, served)
	}
	catalog, err := tools.NewCatalog(ctx, append(specs, skillTools...))
	if err != nil {
		return nil, fmt.Errorf("add the run's own and skill tools: %w", err)
	}
	return &runCapabilities{
		catalog:       catalog,
		skillSnapshot: &served,
		skillDir:      skillDir,
	}, nil
}

// narrowSkills takes out of a snapshot the skills a run lacks tools for.
func narrowSkills(snapshot skills.Snapshot, eligibility skills.EligibilityContext) skills.Snapshot {
	out := skills.CopySnapshot(snapshot)
	for i, view := range out.Skills {
		if !view.Eligible {
			continue
		}
		if narrowed := skills.Evaluate(view.Spec, eligibility); !narrowed.Eligible {
			out.Skills[i] = skills.Disable(view, strings.Join(narrowed.DisabledReasons, ";"))
		}
	}
	return out
}

// toolsOfEligibleSkills keeps the server tools of the skills still in the
// run.
func toolsOfEligibleSkills(specs []core.ToolSpec, snapshot skills.Snapshot) []core.ToolSpec {
	var out []core.ToolSpec
	for _, spec := range specs {
		if slices.ContainsFunc(snapshot.Skills, func(view skills.View) bool {
			return view.Eligible && view.MCP != nil && strings.HasPrefix(spec.Name, skills.MCPToolPrefix(view.Name))
		}) {
			out = append(out, spec)
		}
	}
	return out
}

func keptSpecs(specs []core.ToolSpec, keep func(name string) bool) []core.ToolSpec {
	if keep == nil {
		return specs
	}
	var out []core.ToolSpec
	for _, spec := range specs {
		if keep(spec.Name) {
			out = append(out, spec)
		}
	}
	return out
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
func assembleRunCapabilitiesCatalog(ctx context.Context, deps RuntimeDeps, toolset *tools.Catalog, mcpManager *mcpprovider.Manager) (*tools.Catalog, error) {
	registrySpecs, err := deps.ToolRegistry.ResolveEnabledSpecs(ctx)
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
