package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/stream"
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
	if err := emitProviderDegradedIfNeeded(ctx, f.deps.Store, req, manager.Statuses()); err != nil {
		return nil, err
	}
	return manager, nil
}

func (f *RunnerFactory) getOrCreateMCPManager(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig, sessionOverlay string) (*mcpprovider.Manager, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cachedManager == nil {
		pendingActionStore := mcpprovider.PendingActionStore(f.deps.Store)
		if f.deps.MCPPendingActions != nil {
			pendingActionStore = f.deps.MCPPendingActions
		}
		mgr, err := mcpprovider.NewManager(ctx, providerConfigs, mcpprovider.WithEventCallback(f.providerEventCallback()), mcpprovider.WithTokenStore(f.deps.Store), mcpprovider.WithStore(pendingActionStore))
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

	if f.lastSessionOverlay != sessionOverlay {
		if err := f.cachedManager.ReconcileProviders(ctx, providerConfigs); err != nil {
			return nil, fmt.Errorf("reconcile MCP providers for new session overlay: %w", err)
		}
		f.lastSessionOverlay = sessionOverlay
	}

	return f.cachedManager, nil
}

func (f *RunnerFactory) providerEventCallback() mcpprovider.ProviderEventCallback {
	return func(ev mcpprovider.ProviderEvent) {
		runID := f.currentRunIDValue()

		var sink stream.StreamSink
		if rc, ok := f.registry.Get(runID); ok {
			sink = rc.Sink
		}

		if strings.TrimSpace(runID) == "" {
			runID = systemHotReloadRunID
			sink = nil
		}
		kind, err := streamKindForMCPProviderEvent(ev.Kind)
		if err != nil {
			f.recordEventError(runID, err)
			return
		}

		if _, err := stream.AppendStreamItem(context.Background(), f.deps.Store, sink, stream.StreamItem{
			RunID:     runID,
			Kind:      kind,
			CreatedAt: time.Now().UTC(),
			Payload: map[string]any{
				"provider_name": ev.Provider,
				"transport":     ev.Transport,
				"error":         ev.Error,
				"auth_status":   ev.AuthStatus,
			},
		}); err != nil {
			f.recordEventError(runID, fmt.Errorf("append MCP lifecycle stream item %s: %w", ev.Kind, err))
		}
	}
}

func streamKindForMCPProviderEvent(kind string) (stream.StreamItemKind, error) {
	switch strings.TrimSpace(kind) {
	case "tool_catalog_refreshed":
		return stream.StreamKindMCPToolCatalogRefreshed, nil
	case "tool_catalog_refresh_failed":
		return stream.StreamKindMCPToolCatalogRefreshFailed, nil
	case "provider_added":
		return stream.StreamKindMCPProviderAdded, nil
	case "provider_removed":
		return stream.StreamKindMCPProviderRemoved, nil
	case "provider_restarted":
		return stream.StreamKindMCPProviderRestarted, nil
	case "resource_catalog_refreshed":
		return stream.StreamKindMCPResourceCatalogRefreshed, nil
	case "resource_catalog_refresh_failed":
		return stream.StreamKindMCPResourceCatalogRefreshFailed, nil
	case "prompt_catalog_refreshed":
		return stream.StreamKindMCPPromptCatalogRefreshed, nil
	case "prompt_catalog_refresh_failed":
		return stream.StreamKindMCPPromptCatalogRefreshFailed, nil
	case "auth_status_changed":
		return stream.StreamKindMCPAuthStatusChanged, nil
	default:
		return "", fmt.Errorf("unknown MCP provider event kind %q", kind)
	}
}

func (f *RunnerFactory) recordEventError(runID string, err error) {
	if f == nil || err == nil || strings.TrimSpace(runID) == "" {
		return
	}
	f.eventMu.Lock()
	defer f.eventMu.Unlock()
	if f.eventErrors == nil {
		f.eventErrors = make(map[string]error)
	}
	f.eventErrors[runID] = errors.Join(f.eventErrors[runID], err)
}

func (f *RunnerFactory) consumeEventError(runID string) error {
	if f == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	f.eventMu.Lock()
	defer f.eventMu.Unlock()
	if len(f.eventErrors) == 0 {
		return nil
	}
	err := f.eventErrors[runID]
	delete(f.eventErrors, runID)
	return err
}

func (f *RunnerFactory) ReconcileMCPProviders(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig) error {
	f.mu.Lock()
	mgr := f.cachedManager
	f.mu.Unlock()

	if mgr == nil {
		return nil
	}

	if err := mgr.ReconcileProviders(ctx, providerConfigs); err != nil {
		return err
	}
	if err := f.consumeEventError(systemHotReloadRunID); err != nil {
		return err
	}
	return nil
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
