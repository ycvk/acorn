package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
)

// buildMCPAuxiliaryToolSpecs builds only the MCP resource and prompt tool specs.
// mcp.Manager registers the main MCP tools into the ToolRegistry; the resource
// and prompt tools come from the manager at run time because they are
// session-derived wrappers outside the ToolRegistry lifecycle.
func buildMCPAuxiliaryToolSpecs(ctx context.Context, cfg *config.Config, mcpManager *mcpprovider.Manager) ([]core.ToolSpec, error) {
	var resourceTools, promptTools []einotool.BaseTool
	if mcpManager != nil {
		resourceTools = mcpManager.ResourceTools()
		promptTools = mcpManager.PromptTools()
	}
	resourceSpecs, err := BuildCatalogSpecs(ctx, cfg, "mcp.resource", core.ToolKindMCP, resourceTools)
	if err != nil {
		return nil, err
	}
	promptSpecs, err := BuildCatalogSpecs(ctx, cfg, "mcp.prompt", core.ToolKindMCP, promptTools)
	if err != nil {
		return nil, err
	}
	return append(resourceSpecs, promptSpecs...), nil
}

// buildMCPToolSpec constructs a core.ToolSpec for a single discovered MCP tool,
// applying namespacing, description augmentation, the integration category,
// and the provider's resolved parallel policy. Used by the MCPToolSpecBuilder
// closure handed to mcp.Manager so the unified ToolRegistry receives the spec
// at provider-connect time.
func buildMCPToolSpec(ctx context.Context, cfg *config.Config, providerName string, tool einotool.BaseTool) (core.ToolSpec, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("read MCP tool info for provider %q: %w", providerName, err)
	}
	namespaced, err := NewMCPNamespacedTool(ctx, tool, providerName, info.Name)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("namespace MCP tool %q for provider %q: %w", info.Name, providerName, err)
	}
	return RuntimeToolSpec(ctx, cfg, providerName, core.ToolKindMCP, namespaced)
}

// mcpToolSpecBuilder returns a mcp.ToolSpecBuilder that builds a unified
// registry spec for a discovered MCP tool. It closes over the run config so
// the manager can register tools without a direct config/runtime dependency.
func mcpToolSpecBuilder(cfg *config.Config) mcpprovider.ToolSpecBuilder {
	return func(ctx context.Context, providerName string, tool einotool.BaseTool) (core.ToolSpec, error) {
		return buildMCPToolSpec(ctx, cfg, providerName, tool)
	}
}

func hasEnabledProviders(cfgs []core.ProviderConfig) bool {
	for _, cfg := range cfgs {
		if cfg.Enabled {
			return true
		}
	}
	return false
}

// mcpManagerCache holds the cached mcpprovider.Manager for a RunnerFactory.
// It is the only piece of MCP manager lifecycle state that needs to outlive a
// single run.
type mcpManagerCache struct {
	mu      sync.Mutex
	manager *mcpprovider.Manager
}

func bootstrapRunMCP(ctx context.Context, deps RuntimeDeps, cache *mcpManagerCache, req RunnerBuildRequest) (*mcpprovider.Manager, error) {
	providerConfigs := mcpprovider.ProviderConfigsFromConfig(deps.Config.MCP.Providers)
	if !hasEnabledProviders(providerConfigs) {
		return nil, nil
	}

	manager, err := getOrCreateMCPManager(ctx, deps, cache, providerConfigs)
	if err != nil {
		return nil, err
	}
	manager.SetActiveRunID(req.RunID)
	return manager, nil
}

func getOrCreateMCPManager(ctx context.Context, deps RuntimeDeps, cache *mcpManagerCache, providerConfigs []core.ProviderConfig) (*mcpprovider.Manager, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.manager == nil {
		return createMCPManager(ctx, deps, cache, providerConfigs)
	}
	return cache.manager, nil
}

func createMCPManager(ctx context.Context, deps RuntimeDeps, cache *mcpManagerCache, providerConfigs []core.ProviderConfig) (*mcpprovider.Manager, error) {
	pendingActionStore := core.SessionStore(deps.Store)
	if deps.MCPPendingActions != nil {
		pendingActionStore = deps.MCPPendingActions
	}
	opts := []mcpprovider.ManagerOption{
		mcpprovider.WithTokenStore(deps.Store),
		mcpprovider.WithStore(pendingActionStore),
		mcpprovider.WithToolRegistry(deps.ToolRegistry, mcpToolSpecBuilder(deps.Config)),
	}
	mgr, err := mcpprovider.NewManager(ctx, providerConfigs, opts...)
	if err != nil {
		return nil, err
	}
	cache.manager = mgr
	return mgr, nil
}

func closeMCPCache(cache *mcpManagerCache) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	var closeErr error
	if cache.manager != nil {
		closeErr = errors.Join(closeErr, cache.manager.Close())
		cache.manager = nil
	}
	return closeErr
}

const (
	mcpNamespacePrefix = "mcp__"
	mcpNamespaceSep    = "__"
)

func mcpToolName(provider, toolName string) string {
	return mcpNamespacePrefix + mcpprovider.ToolNamespace(provider) + mcpNamespaceSep + toolName
}

func augmentDescription(desc, provider string) string {
	if strings.TrimSpace(desc) == "" {
		return fmt.Sprintf("Provided by MCP server: %s", provider)
	}
	return desc + fmt.Sprintf("\n\nProvided by MCP server: %s", provider)
}

type mcpNamespacedTool struct {
	inner         einotool.BaseTool
	invokable     einotool.InvokableTool
	prefixedName  string
	augmentedDesc string
}

func NewMCPNamespacedTool(ctx context.Context, inner einotool.BaseTool, provider, originalToolName string) (*mcpNamespacedTool, error) {
	invokable, ok := inner.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("MCP tool %q is not invokable", originalToolName)
	}
	info, err := inner.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tool info for MCP namespacing: %w", err)
	}
	prefixed := mcpToolName(provider, originalToolName)
	augDesc := augmentDescription(info.Desc, provider)
	return &mcpNamespacedTool{
		inner:         inner,
		invokable:     invokable,
		prefixedName:  prefixed,
		augmentedDesc: augDesc,
	}, nil
}

func (t *mcpNamespacedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &schema.ToolInfo{
		Name:        t.prefixedName,
		Desc:        t.augmentedDesc,
		ParamsOneOf: info.ParamsOneOf,
	}, nil
}

func (t *mcpNamespacedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}
