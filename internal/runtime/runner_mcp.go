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

func buildMCPToolSpecs(ctx context.Context, cfg *config.Config, mcpManager *mcpprovider.Manager) ([]core.ToolSpec, error) {
	var resourceTools, promptTools []einotool.BaseTool
	if mcpManager != nil {
		resourceTools = mcpManager.ResourceTools()
		promptTools = mcpManager.PromptTools()
	}
	specs, err := buildMCPRegistrationsSpecs(ctx, cfg, mcpManager)
	if err != nil {
		return nil, err
	}
	resourceSpecs, err := BuildCatalogSpecs(ctx, cfg, "mcp.resource", core.ToolKindMCP, resourceTools)
	if err != nil {
		return nil, err
	}
	promptSpecs, err := BuildCatalogSpecs(ctx, cfg, "mcp.prompt", core.ToolKindMCP, promptTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, resourceSpecs...)
	specs = append(specs, promptSpecs...)
	return specs, nil
}

func buildMCPRegistrationsSpecs(ctx context.Context, cfg *config.Config, mcpManager *mcpprovider.Manager) ([]core.ToolSpec, error) {
	var specs []core.ToolSpec
	for _, registration := range mcpManagerRegistrations(mcpManager) {
		spec, err := buildMCPRegistrationSpec(ctx, cfg, registration)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func buildMCPRegistrationSpec(ctx context.Context, cfg *config.Config, registration mcpprovider.ToolRegistration) (core.ToolSpec, error) {
	info, err := registration.Tool.Info(ctx)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("read MCP tool info for provider %q: %w", registration.ProviderName, err)
	}
	namespaced, err := NewMCPNamespacedTool(ctx, registration.Tool, registration.ProviderName, info.Name)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("namespace MCP tool %q for provider %q: %w", info.Name, registration.ProviderName, err)
	}
	spec, err := RuntimeToolSpec(ctx, cfg, registration.ProviderName, core.ToolKindMCP, namespaced)
	if err != nil {
		return core.ToolSpec{}, err
	}
	parallelPolicy, err := MCPToolParallelPolicy(cfg, registration.ProviderName)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("resolve MCP tool safety for provider %q: %w", registration.ProviderName, err)
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
	mgr, err := mcpprovider.NewManager(ctx, providerConfigs, mcpprovider.WithTokenStore(deps.Store), mcpprovider.WithStore(pendingActionStore))
	if err != nil {
		return nil, err
	}
	cache.manager = mgr
	cache.lastSessionOverlay = sessionOverlay
	return mgr, nil
}

func reconcileMCPProviders(ctx context.Context, cache *mcpManagerCache, providerConfigs []mcpprovider.ProviderConfig) error {
	cache.mu.Lock()
	mgr := cache.manager
	cache.mu.Unlock()

	if mgr == nil {
		return nil
	}

	return mgr.ReconcileProviders(ctx, providerConfigs)
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
