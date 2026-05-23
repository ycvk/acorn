package toolfactory

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/tooling"
)

func BuildCatalogSpecs(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tooling.ToolKind,
	profiles []tooling.ToolProfile,
	tools []einotool.BaseTool,
) ([]tooling.ToolSpec, error) {
	specs := make([]tooling.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		spec, err := RuntimeToolSpec(ctx, cfg, source, kind, profiles, tool)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func RuntimeToolSpec(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tooling.ToolKind,
	profiles []tooling.ToolProfile,
	tool einotool.BaseTool,
) (tooling.ToolSpec, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return tooling.ToolSpec{}, fmt.Errorf("read tool info for %s spec: %w", source, err)
	}
	if info == nil {
		return tooling.ToolSpec{}, fmt.Errorf("read tool info for %s spec: nil ToolInfo", source)
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		return tooling.ToolSpec{}, fmt.Errorf("%s tool has empty name", source)
	}

	if localSpec, ok := tooling.ConfiguredLocalSpec(cfg, name); ok {
		localSpec.Tool = tool
		return localSpec, nil
	}

	spec := tooling.ToolSpec{
		ToolContract: tooling.ToolContract{
			Name:          name,
			Source:        source,
			Kind:          kind,
			Category:      tooling.ToolCategoryInspect,
			ResourceScope: tooling.ResourceScopeWorkspaceFile,
			Profiles:      append([]tooling.ToolProfile(nil), profiles...),
			PlanPolicy:    tooling.PlanPolicyNone,
			FactPolicy:    tooling.FactPolicySuppress,
			Loading:       tooling.EagerLoadingPolicy(),
			Execution: tooling.ToolExecutionPolicy{
				ParallelPolicy: tooling.ParallelPolicyReadOnly,
				SideEffects:    []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace},
			},
			Result:     tooling.InlineResultPolicy(0),
			Boundary:   tooling.ToolResultBoundaryPolicy(),
			Projection: tooling.ActivityProjectionPolicy(),
		},
		Tool: tool,
	}

	switch name {
	case "delegate_task":
		spec.Kind = tooling.ToolKindSkill
		spec.Category = tooling.ToolCategorySkill
		spec.ResourceScope = tooling.ResourceScopeSkill
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectSkillRead}
		spec.PlanPolicy = tooling.PlanPolicyRequireActivePlan
	case "load_tools":
		spec.Kind = tooling.ToolKindNative
		spec.Category = tooling.ToolCategoryInspect
		spec.ResourceScope = tooling.ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace}
	case "update_working_checkpoint", "clear_working_checkpoint":
		spec.Kind = tooling.ToolKindMemory
		spec.Category = tooling.ToolCategoryMemory
		spec.ResourceScope = tooling.ResourceScopeMemory
		spec.Loading = tooling.DeferredLoadingPolicy("working_state_tool")
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectMemoryWrite}
	case "memory_search", "memory_read_file", "memory_list_files":
		spec.Kind = tooling.ToolKindMemory
		spec.Category = tooling.ToolCategoryMemory
		spec.ResourceScope = tooling.ResourceScopeMemory
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectMemoryRead}
		spec.FactPolicy = tooling.FactPolicySuppress
	case "memory_create_file", "memory_replace_span":
		spec.Kind = tooling.ToolKindMemory
		spec.Category = tooling.ToolCategoryMemory
		spec.ResourceScope = tooling.ResourceScopeMemory
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyWriteScoped
		spec.Execution.PathArg = "path"
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectMemoryWrite}
		spec.FactPolicy = tooling.FactPolicySuppress
	case "skill_list", "skill_view":
		spec.Kind = tooling.ToolKindSkill
		spec.Category = tooling.ToolCategorySkill
		spec.ResourceScope = tooling.ResourceScopeSkill
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectSkillRead}
	default:
		switch kind {
		case tooling.ToolKindMCP, tooling.ToolKindMCPResource, tooling.ToolKindMCPPrompt:
			spec.Kind = kind
			spec.Category = tooling.ToolCategoryIntegration
			spec.ResourceScope = tooling.ResourceScopeMCP
			spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
			spec.Execution.PathArg = "path"
			spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectIntegration}
			spec.FactPolicy = tooling.FactPolicyAuto
			if kind == tooling.ToolKindMCPResource || kind == tooling.ToolKindMCPPrompt {
				spec.Loading = tooling.DeferredLoadingPolicy("deferred_mcp_catalog")
			}
		default:
			spec.Category = tooling.ToolCategoryInspect
			spec.ResourceScope = tooling.ResourceScopeWorkspaceFile
			spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
			spec.Execution.PathArg = "path"
			spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace}
			spec.FactPolicy = tooling.FactPolicyAuto
		}
	}
	return spec, nil
}

func McpToolParallelPolicy(cfg *config.Config, providerName string) (tooling.ParallelPolicy, error) {
	if cfg == nil {
		return "", fmt.Errorf("resolve MCP tool safety for provider %q: config is required", strings.TrimSpace(providerName))
	}
	for _, provider := range cfg.MCP.Providers {
		if strings.TrimSpace(provider.Name) != strings.TrimSpace(providerName) {
			continue
		}
		if strings.TrimSpace(provider.ToolSafety) == "" {
			return "", fmt.Errorf("mcp provider %q must declare tool_safety", strings.TrimSpace(providerName))
		}
		return tooling.ParseParallelPolicy(provider.ToolSafety)
	}
	return "", fmt.Errorf("mcp provider %q is not configured", strings.TrimSpace(providerName))
}
