package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/tools"
)

func (f *RunnerFactory) buildMCPToolSpecs(ctx context.Context, mcpManager *mcpprovider.Manager) ([]tools.ToolSpec, error) {
	var resourceTools, promptTools []einotool.BaseTool
	if mcpManager != nil {
		resourceTools = mcpManager.ResourceTools()
		promptTools = mcpManager.PromptTools()
	}
	specs, err := f.buildMCPRegistrationsSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	resourceSpecs, err := BuildCatalogSpecs(ctx, f.deps.Config, "mcp.resource", tools.ToolKindMCP, resourceTools)
	if err != nil {
		return nil, err
	}
	promptSpecs, err := BuildCatalogSpecs(ctx, f.deps.Config, "mcp.prompt", tools.ToolKindMCP, promptTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, resourceSpecs...)
	specs = append(specs, promptSpecs...)
	return specs, nil
}

func (f *RunnerFactory) buildMCPRegistrationsSpecs(ctx context.Context, mcpManager *mcpprovider.Manager) ([]tools.ToolSpec, error) {
	var specs []tools.ToolSpec
	for _, registration := range mcpManagerRegistrations(mcpManager) {
		spec, err := f.buildMCPRegistrationSpec(ctx, registration)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (f *RunnerFactory) buildMCPRegistrationSpec(ctx context.Context, registration mcpprovider.ToolRegistration) (tools.ToolSpec, error) {
	info, err := registration.Tool.Info(ctx)
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("read MCP tool info for provider %q: %w", registration.ProviderName, err)
	}
	namespaced, err := NewMCPNamespacedTool(ctx, registration.Tool, registration.ProviderName, info.Name)
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("namespace MCP tool %q for provider %q: %w", info.Name, registration.ProviderName, err)
	}
	spec, err := RuntimeToolSpec(ctx, f.deps.Config, registration.ProviderName, tools.ToolKindMCP, namespaced)
	if err != nil {
		return tools.ToolSpec{}, err
	}
	parallelPolicy, err := MCPToolParallelPolicy(f.deps.Config, registration.ProviderName)
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("resolve MCP tool safety for provider %q: %w", registration.ProviderName, err)
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

func (f *RunnerFactory) bootstrapRunMCP(ctx context.Context, req RunnerBuildRequest) (*mcpprovider.Manager, error) {
	providerConfigs := mcpprovider.ProviderConfigsFromConfig(f.deps.Config.MCP.Providers)
	if !hasEnabledProviders(providerConfigs) {
		return nil, nil
	}

	sessionOverlay := ""
	if strings.TrimSpace(req.SessionID) != "" {
		sessionOverlay = req.SessionID
	}
	manager, err := f.getOrCreateMCPManager(ctx, providerConfigs, sessionOverlay)
	if err != nil {
		return nil, err
	}
	manager.SetActiveRunID(req.RunID)
	return manager, nil
}

func (f *RunnerFactory) getOrCreateMCPManager(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig, sessionOverlay string) (*mcpprovider.Manager, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cachedManager == nil {
		return f.createMCPManager(ctx, providerConfigs, sessionOverlay)
	}
	if f.lastSessionOverlay != sessionOverlay {
		if err := f.cachedManager.ReconcileProviders(ctx, providerConfigs); err != nil {
			return nil, fmt.Errorf("reconcile MCP providers for new session overlay: %w", err)
		}
		f.lastSessionOverlay = sessionOverlay
	}
	return f.cachedManager, nil
}

func (f *RunnerFactory) createMCPManager(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig, sessionOverlay string) (*mcpprovider.Manager, error) {
	pendingActionStore := mcpprovider.PendingActionStore(f.deps.Store)
	if f.deps.MCPPendingActions != nil {
		pendingActionStore = f.deps.MCPPendingActions
	}
	mgr, err := mcpprovider.NewManager(ctx, providerConfigs, mcpprovider.WithTokenStore(f.deps.Store), mcpprovider.WithStore(pendingActionStore))
	if err != nil {
		return nil, err
	}
	f.cachedManager = mgr
	f.lastSessionOverlay = sessionOverlay
	return mgr, nil
}

func (f *RunnerFactory) ReconcileMCPProviders(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig) error {
	f.mu.Lock()
	mgr := f.cachedManager
	f.mu.Unlock()

	if mgr == nil {
		return nil
	}

	return mgr.ReconcileProviders(ctx, providerConfigs)
}

func (f *RunnerFactory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var closeErr error
	if f.cachedManager != nil {
		closeErr = errors.Join(closeErr, f.cachedManager.Close())
		f.cachedManager = nil
		f.lastSessionOverlay = ""
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
