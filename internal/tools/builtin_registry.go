package tools

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// builtinToolOrder is the canonical list of dynamically-registered built-in
// tools (ask_operator, memory, worldstate, skill). It is the
// single source of truth for built-in tool identity: BuiltinToolNames and the
// runtime spec resolver (tool.RuntimeToolSpec via BuiltinToolSpec) both derive
// from it, so adding a built-in tool means editing this one place.
//
// Static local tools (artifact_*, ask_operator, search_runs, ...) are declared
// separately in localToolNames/configuredLocalSpec.
var builtinToolOrder = []string{
	"memory_search",
	"memory_read_file",
	"memory_list_files",
	"memory_create_file",
	"memory_replace_span",
	"remember",
	"search_runs",
	"worldstate_update",
	"worldstate_load",
	"skill_list",
	"skill_view",
	"skill_create",
	"ask_operator",
}

// builtinToolContract returns the contract template (without Source/Profiles,
// which are caller-supplied) for a built-in tool. ok is false for any name that
// is not a built-in tool (e.g. MCP tools), which callers resolve elsewhere.
func builtinToolContract(name string) (core.ToolContract, bool) {
	c := core.ToolContract{
		Name:    name,
		Loading: core.EagerLoadingPolicy(),
	}
	switch name {
	case "ask_operator":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryIntegration
	case "memory_search", "memory_read_file", "memory_list_files":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
	case "memory_create_file", "memory_replace_span":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
	case "remember":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
	case "search_runs":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryInspect
	case "worldstate_update":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryMemory
	case "worldstate_load":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryInspect
	case "skill_list", "skill_view":
		c.Kind = core.ToolKindSkill
		c.Category = core.ToolCategorySkill
	case "skill_create":
		c.Kind = core.ToolKindSkill
		c.Category = core.ToolCategorySkill
	default:
		return core.ToolContract{}, false
	}
	return c, true
}

// BuiltinToolSpec resolves the full contract for a built-in tool, applying the
// caller-supplied source to the canonical contract template. It returns ok=false
// for names that are not built-in toolset.
func BuiltinToolSpec(name, source string) (core.ToolContract, bool) {
	c, ok := builtinToolContract(name)
	if !ok {
		return core.ToolContract{}, false
	}
	c.Source = source
	return c, true
}

// BuiltinToolNames returns the built-in tools, which are all eager-loaded and
// therefore always eligible for skill matching.
func BuiltinToolNames() []string {
	return append([]string(nil), builtinToolOrder...)
}

// nativeToolBuilder maps a static local tool name to the single-tool builder
// that produces its einotool.BaseTool. Each entry mirrors the call the
// corresponding group builder (buildWorkspaceTools, buildMutationTools, ...)
// would make for that single name, so the registry produces the same tool
// instances the existing Catalog path does.
//
// A nil service dependency makes the builder return (nil, nil): the tool is
// registered (so its contract/health is visible) but Resolve yields no
// instance for it, matching the existing Catalog behavior of silently omitting
// tools whose backing service is absent.
func nativeToolBuilder(name string, cfg CatalogConfig) func(context.Context, core.RunContext) (einotool.BaseTool, error) {
	switch name {
	case "artifact_write":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildArtifactWriteTool(cfg.ArtifactService, cfg.ArtifactContext)
		}
	case "artifact_read":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildArtifactReadTool(cfg.ArtifactService)
		}
	case "artifact_list":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildArtifactListTool(cfg.ArtifactService, cfg.ArtifactContext)
		}
	case "ask_operator":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.OperatorStore == nil {
				return nil, nil
			}
			return buildAskOperatorTool(cfg.OperatorStore, cfg.OperatorContext)
		}
	case "search_runs":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.RunSearchStore == nil {
				return nil, nil
			}
			return buildSearchRunsTool(cfg.RunSearchStore)
		}
	case "worldstate_update":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.WorldStateUpdater == nil {
				return nil, nil
			}
			return buildWorldStateUpdateTool(cfg.WorldStateUpdater)
		}
	case "worldstate_load":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.WorldStateUpdater == nil {
				return nil, nil
			}
			return buildWorldStateLoadTool(cfg.WorldStateUpdater)
		}
	case "web_fetch":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.WebFetchService == nil || cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildWebFetchTool(cfg.WebFetchService, cfg.ArtifactService, cfg.ArtifactContext)
		}
	case "web_search":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.WebSearchService == nil || cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildWebSearchTool(cfg.WebSearchService, cfg.ArtifactService, cfg.ArtifactContext)
		}
	case "browser":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.BrowserService == nil || cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildBrowserTool(cfg.BrowserService, cfg.ArtifactService, cfg.ArtifactContext)
		}
	default:
		return nil
	}
}

// RegisterNativeTools registers every eager-loaded static local tool declared by
// localToolNames/configuredLocalSpec into the core.ToolRegistry. Deferred-loaded
// tools (web_fetch, web_search, browser) are excluded: they depend on per-run
// services (web access, browser) constructed at buildRun time, so they cannot
// be resolved at wire time. They are contributed by the runtime toolset
// catalog instead, which builds them per run from live services.
//
// cfg may be zero-valued: tools whose backing service is nil are still
// registered (their contract and health are visible) but their Factory returns
// (nil, nil), so Resolve omits them. This mirrors how the Catalog silently
// drops tools when their service is absent.
func RegisterNativeTools(registry core.ToolRegistry, cfg CatalogConfig) error {
	if registry == nil {
		return fmt.Errorf("RegisterNativeTools: registry is nil")
	}
	// localToolNames is the canonical name list in canonical order; reusing it
	// keeps the registry from drifting from configuredLocalSpec.
	for _, name := range localToolNames {
		spec := configuredLocalSpec(name)
		// Skip deferred-loaded tools: they depend on per-run services and are
		// contributed by the runtime toolset catalog, not the wire-time registry.
		if spec.Loading.Mode == core.ToolLoadingModeDeferred {
			continue
		}
		build := nativeToolBuilder(name, cfg)
		if build == nil {
			// Unknown name: skip rather than fail the whole registration so a
			// future localToolNames entry without a builder doesn't block the
			// known tools. This is defensive; localToolNames and
			// nativeToolBuilder are kept in sync.
			continue
		}
		spec.Factory = core.ToolFactory(build)
		if err := registry.Register(spec); err != nil {
			return fmt.Errorf("RegisterNativeTools: register %q: %w", name, err)
		}
	}
	return nil
}
