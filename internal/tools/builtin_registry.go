package tools

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
)

// builtinToolOrder is the canonical list of dynamically-registered built-in
// tools (delegate_task, load_tools, working-state, memory, skill). It is the
// single source of truth for built-in tool identity: BuiltinToolNames and the
// runtime spec resolver (tool.RuntimeToolSpec via BuiltinToolSpec) both derive
// from it, so adding a built-in tool means editing this one place.
//
// Static local tools (read_file, create_file, run_command, ...) are declared
// separately in localToolDefs/configuredLocalSpec.
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
	"load_tools",
	"ask_operator",
	"update_working_checkpoint",
	"clear_working_checkpoint",
}

// builtinToolContract returns the contract template (without Source/Profiles,
// which are caller-supplied) for a built-in tool. ok is false for any name that
// is not a built-in tool (e.g. MCP tools), which callers resolve elsewhere.
func builtinToolContract(name string) (core.ToolContract, bool) {
	c := core.ToolContract{
		Name:      name,
		Loading:   core.EagerLoadingPolicy(),
		Execution: core.ToolExecutionPolicy{ParallelPolicy: core.ParallelPolicyReadOnly},
	}
	switch name {
	case "load_tools":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryInspect
		c.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "ask_operator":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryIntegration
		c.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "update_working_checkpoint", "clear_working_checkpoint":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
		c.Loading = core.DeferredLoadingPolicy("working_state_tool")
		c.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "memory_search", "memory_read_file", "memory_list_files":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
		c.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
	case "memory_create_file", "memory_replace_span":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
		c.Execution.ParallelPolicy = core.ParallelPolicySerial
		c.Execution.PathArg = "path"
	case "remember":
		c.Kind = core.ToolKindMemory
		c.Category = core.ToolCategoryMemory
		c.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "search_runs":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryInspect
		c.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
	case "worldstate_update":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryMemory
		c.Execution.ParallelPolicy = core.ParallelPolicySerial
	case "worldstate_load":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryInspect
		c.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
	case "skill_list", "skill_view":
		c.Kind = core.ToolKindSkill
		c.Category = core.ToolCategorySkill
		c.Execution.ParallelPolicy = core.ParallelPolicyReadOnly
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

// BuiltinToolNames returns the built-in tools that are always eligible for skill
// matching, i.e. the eager-loaded built-ins. Deferred built-ins (working-state
// tools) are loaded on demand and are intentionally excluded. The list derives
// from builtinToolOrder, so it never drifts from the contract registry.
func BuiltinToolNames() []string {
	names := make([]string, 0, len(builtinToolOrder))
	for _, name := range builtinToolOrder {
		contract, ok := builtinToolContract(name)
		if ok && contract.Loading.Mode == core.ToolLoadingModeEager {
			names = append(names, name)
		}
	}
	return names
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
	case "read_file":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildReadFileTool(cfg.Workspace)
		}
	case "list_files":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildListFilesTool(cfg.Workspace)
		}
	case "search_text":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildSearchTextTool(cfg.Workspace)
		}
	case "inspect_git_status":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildInspectGitStatusTool(cfg.Workspace)
		}
	case "inspect_git_diff":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildInspectGitDiffTool(cfg.Workspace)
		}
	case "git_summary":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil || cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildGitSummaryTool(cfg.Workspace, cfg.ArtifactService, cfg.ArtifactContext)
		}
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
	case "create_file":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildCreateFileTool(cfg.Workspace)
		}
	case "replace_span":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildReplaceSpanTool(cfg.Workspace)
		}
	case "apply_unified_patch":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildApplyUnifiedPatchTool(cfg.Workspace)
		}
	case "multi_edit":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildMultiEditTool(cfg.Workspace)
		}
	case "rollback_workspace_checkpoint":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildRollbackWorkspaceCheckpointTool(cfg.Workspace)
		}
	case "run_command":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil {
				return nil, nil
			}
			return buildRunCommandTool(cfg.Workspace)
		}
	case "run_verification":
		return func(_ context.Context, _ core.RunContext) (einotool.BaseTool, error) {
			if cfg.Workspace == nil || cfg.ArtifactService == nil {
				return nil, nil
			}
			return buildRunVerificationTool(cfg.Workspace, cfg.ArtifactService, cfg.ArtifactContext)
		}
	default:
		return nil
	}
}

// RegisterNativeTools registers every eager-loaded static local tool declared by
// localToolDefs/configuredLocalSpec into the core.ToolRegistry. Deferred-loaded
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
	// localToolDefs returns the canonical name list in canonical order; reuse it
	// rather than re-declaring the names so the registry never drifts from
	// configuredLocalSpec. localToolDefs dereferences its *config.Config, so we
	// synthesize a minimal one whose Mutation/RunCommand Disabled flags mirror
	// CatalogConfig (Disabled == !enabled); the always-on baseline tools are
	// governed by the per-tool Factory's nil-service guard instead.
	toolCfg := &config.Config{}
	toolCfg.Tools.Mutation.Disabled = !cfg.MutationEnabled
	toolCfg.Tools.RunCommand.Disabled = !cfg.RunCommandEnabled
	for _, def := range localToolDefs(toolCfg) {
		spec := configuredLocalSpec(def.name, def.enabled)
		// Skip deferred-loaded tools: they depend on per-run services and are
		// contributed by the runtime toolset catalog, not the wire-time registry.
		if spec.Loading.Mode == core.ToolLoadingModeDeferred {
			continue
		}
		build := nativeToolBuilder(def.name, cfg)
		if build == nil {
			// Unknown name: skip rather than fail the whole registration so a
			// future localToolDefs entry without a builder doesn't block the
			// known tools. This is defensive; localToolDefs and
			// nativeToolBuilder are kept in sync.
			continue
		}
		spec.Factory = core.ToolFactory(build)
		if err := registry.Register(spec); err != nil {
			return fmt.Errorf("RegisterNativeTools: register %q: %w", def.name, err)
		}
	}
	return nil
}
