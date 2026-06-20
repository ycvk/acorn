package runtime

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

type runCapabilities struct {
	catalog       *tooling.Catalog
	skillSnapshot *skills.Snapshot
	stableSkills  []skills.Spec
	close         func() error
}

func (c *runCapabilities) Close() error {
	if c == nil || c.close == nil {
		return nil
	}
	return c.close()
}

func (f *RunnerFactory) buildRunCapabilities(ctx context.Context, sessionID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	childExec, err := f.newChildAgentExecutor()
	if err != nil {
		return nil, err
	}
	toolset, err := f.buildRunToolset(ctx, sessionID, childExec)
	if err != nil {
		return nil, err
	}
	specs := append([]tooling.ToolSpec(nil), toolset.Catalog().Specs()...)
	mcpSpecs, err := f.buildMCPToolSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, err
	}
	skillSnapshot, err := loadStableSkillSnapshot(ctx, f.deps.Loader, skillEligibilityContextFromCatalog(catalog))
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

func (f *RunnerFactory) buildMCPToolSpecs(ctx context.Context, mcpManager *mcpprovider.Manager) ([]tooling.ToolSpec, error) {
	var resourceTools, promptTools []einotool.BaseTool
	if mcpManager != nil {
		resourceTools = mcpManager.ResourceTools()
		promptTools = mcpManager.PromptTools()
	}
	specs, err := f.buildMCPRegistrationsSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	resourceSpecs, err := tool.BuildCatalogSpecs(ctx, f.deps.Config, "mcp.resource", tooling.ToolKindMCPResource, []tooling.ToolProfile{tooling.ToolProfileRun}, resourceTools)
	if err != nil {
		return nil, err
	}
	promptSpecs, err := tool.BuildCatalogSpecs(ctx, f.deps.Config, "mcp.prompt", tooling.ToolKindMCPPrompt, []tooling.ToolProfile{tooling.ToolProfileRun}, promptTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, resourceSpecs...)
	specs = append(specs, promptSpecs...)
	return specs, nil
}

func (f *RunnerFactory) buildMCPRegistrationsSpecs(ctx context.Context, mcpManager *mcpprovider.Manager) ([]tooling.ToolSpec, error) {
	var specs []tooling.ToolSpec
	for _, registration := range mcpManagerRegistrations(mcpManager) {
		spec, err := f.buildMCPRegistrationSpec(ctx, registration)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (f *RunnerFactory) buildMCPRegistrationSpec(ctx context.Context, registration mcpprovider.ToolRegistration) (tooling.ToolSpec, error) {
	info, err := registration.Tool.Info(ctx)
	if err != nil {
		return tooling.ToolSpec{}, fmt.Errorf("read MCP tool info for provider %q: %w", registration.ProviderName, err)
	}
	namespaced, err := tool.NewMCPNamespacedTool(ctx, registration.Tool, registration.ProviderName, info.Name)
	if err != nil {
		return tooling.ToolSpec{}, fmt.Errorf("namespace MCP tool %q for provider %q: %w", info.Name, registration.ProviderName, err)
	}
	spec, err := tool.RuntimeToolSpec(ctx, f.deps.Config, registration.ProviderName, tooling.ToolKindMCP, []tooling.ToolProfile{tooling.ToolProfileRun}, namespaced)
	if err != nil {
		return tooling.ToolSpec{}, err
	}
	parallelPolicy, err := tool.MCPToolParallelPolicy(f.deps.Config, registration.ProviderName)
	if err != nil {
		return tooling.ToolSpec{}, fmt.Errorf("resolve MCP tool safety for provider %q: %w", registration.ProviderName, err)
	}
	spec.Execution.ParallelPolicy = parallelPolicy
	return spec, nil
}

func mcpManagerRegistrations(manager *mcpprovider.Manager) []mcpprovider.ToolRegistration {
	if manager == nil {
		return nil
	}
	return manager.Registrations()
}
