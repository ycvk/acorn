package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
)

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
	childExec, err := f.newChildAgentExecutor()
	if err != nil {
		return nil, err
	}
	mgr.SetSamplingExecutor(subagentExecutorAdapter{exec: childExec})
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
