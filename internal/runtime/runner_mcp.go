package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
)

// buildMCPAuxiliaryToolSpecs builds only the MCP resource and prompt tool specs
// (not the main tool registrations). It is used on the unified-registry path,
// where main MCP tools are already registered into the ToolRegistry by
// mcp.Manager; the resource/prompt tools are still sourced from the manager at
// run time because they are session-derived wrappers, not part of the
// ToolRegistry lifecycle.
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
	spec, err := RuntimeToolSpec(ctx, cfg, providerName, core.ToolKindMCP, namespaced)
	if err != nil {
		return core.ToolSpec{}, err
	}
	parallelPolicy, err := MCPToolParallelPolicy(cfg, providerName)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("resolve MCP tool safety for provider %q: %w", providerName, err)
	}
	spec.Execution.ParallelPolicy = parallelPolicy
	return spec, nil
}

// mcpToolSpecBuilder returns a mcp.ToolSpecBuilder that builds a unified
// registry spec for a discovered MCP tool. It closes over the run config so
// the manager can register tools without a direct config/runtime dependency.
func mcpToolSpecBuilder(cfg *config.Config) mcpprovider.ToolSpecBuilder {
	return func(ctx context.Context, providerName string, tool einotool.BaseTool) (core.ToolSpec, error) {
		return buildMCPToolSpec(ctx, cfg, providerName, tool)
	}
}

func hasEnabledProviders(cfgs []mcpprovider.ProviderConfig) bool {
	for _, cfg := range cfgs {
		if cfg.Enabled {
			return true
		}
	}
	return false
}

// mcpManagerCache holds the cached mcpprovider.Manager for a RunnerFactory,
// reconciled across session overlays. It replaces the former MCPAssembler
// struct; the factory stays a thin coordinator and the cache is the only piece
// of MCP manager lifecycle state that needs to outlive a single run.
type mcpManagerCache struct {
	mu                 sync.Mutex
	manager            *mcpprovider.Manager
	lastSessionOverlay string
}

func bootstrapRunMCP(ctx context.Context, deps RuntimeDeps, cache *mcpManagerCache, req RunnerBuildRequest) (*mcpprovider.Manager, error) {
	providerConfigs := mcpprovider.ProviderConfigsFromConfig(deps.Config.MCP.Providers)
	if !hasEnabledProviders(providerConfigs) {
		return nil, nil
	}

	sessionOverlay := ""
	if strings.TrimSpace(req.SessionID) != "" {
		sessionOverlay = req.SessionID
	}
	manager, err := getOrCreateMCPManager(ctx, deps, cache, providerConfigs, sessionOverlay)
	if err != nil {
		return nil, err
	}
	manager.SetActiveRunID(req.RunID)
	return manager, nil
}

func getOrCreateMCPManager(ctx context.Context, deps RuntimeDeps, cache *mcpManagerCache, providerConfigs []mcpprovider.ProviderConfig, sessionOverlay string) (*mcpprovider.Manager, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.manager == nil {
		return createMCPManager(ctx, deps, cache, providerConfigs, sessionOverlay)
	}
	if cache.lastSessionOverlay != sessionOverlay {
		if err := cache.manager.ReconcileProviders(ctx, providerConfigs); err != nil {
			return nil, fmt.Errorf("reconcile MCP providers for new session overlay: %w", err)
		}
		cache.lastSessionOverlay = sessionOverlay
	}
	return cache.manager, nil
}

func createMCPManager(ctx context.Context, deps RuntimeDeps, cache *mcpManagerCache, providerConfigs []mcpprovider.ProviderConfig, sessionOverlay string) (*mcpprovider.Manager, error) {
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
	cache.lastSessionOverlay = sessionOverlay
	return mgr, nil
}

func closeMCPCache(cache *mcpManagerCache) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	var closeErr error
	if cache.manager != nil {
		closeErr = errors.Join(closeErr, cache.manager.Close())
		cache.manager = nil
		cache.lastSessionOverlay = ""
	}
	return closeErr
}

const (
	mcpNamespacePrefix = "mcp__"
	mcpNamespaceSep    = "__"
)

func mcpToolName(provider, toolName string) string {
	return mcpNamespacePrefix + sanitizeMCPProviderName(provider) + mcpNamespaceSep + toolName
}

func sanitizeMCPProviderName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := strings.Trim(b.String(), "-_ .")
	if result == "" {
		return "provider"
	}
	return result
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

// samplingExecutorAdapter adapts einomodel.BaseChatModel to the
// mcpprovider.SamplingExecutor interface. Each sampling request is a
// single Generate call — no run lifecycle, no store persistence.
type samplingExecutorAdapter struct {
	model einomodel.BaseChatModel
}

func (a samplingExecutorAdapter) ExecuteMessages(ctx context.Context, messages []*schema.Message) (string, error) {
	resp, err := a.model.Generate(ctx, messages)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
