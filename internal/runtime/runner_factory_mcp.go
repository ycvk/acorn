package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
)

const systemHotReloadRunID = "_system_hot_reload"

func (r *ActiveRunner) Close() error {
	if r == nil {
		return nil
	}
	var closeErr error
	if r != nil && r.CloseRunTools != nil {
		closeErr = r.CloseRunTools()
		r.CloseRunTools = nil
	}
	if r.Factory != nil && r.RunID != "" {
		r.Factory.registry.Clear(r.RunID)
		r.Factory.ClearCurrentRunID(r.RunID)
	}
	return closeErr
}

func (f *RunnerFactory) setCurrentRunID(runID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentRunID.Store(runID)
}

func (f *RunnerFactory) ClearCurrentRunID(runID string) {
	if f == nil || runID == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.currentRunIDValue() == runID {
		f.currentRunID.Store("")
	}
}

func (f *RunnerFactory) currentRunIDValue() string {
	if f == nil {
		return ""
	}
	value := f.currentRunID.Load()
	runID, ok := value.(string)
	if !ok {
		return ""
	}
	return runID
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

		childExec := f.newChildAgentExecutor()
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

		var sink StreamSink
		if rc, ok := f.registry.Get(runID); ok {
			sink = rc.Sink
		}

		if strings.TrimSpace(runID) == "" {
			runID = systemHotReloadRunID
			sink = nil
		}

		var kind StreamItemKind
		switch ev.Kind {
		case "tool_catalog_refreshed":
			kind = StreamKindMCPToolCatalogRefreshed
		case "tool_catalog_refresh_failed":
			kind = StreamKindMCPToolCatalogRefreshFailed
		case "provider_added":
			kind = StreamKindMCPProviderAdded
		case "provider_removed":
			kind = StreamKindMCPProviderRemoved
		case "provider_restarted":
			kind = StreamKindMCPProviderRestarted
		case "resource_catalog_refreshed":
			kind = StreamKindMCPResourceCatalogRefreshed
		case "resource_catalog_refresh_failed":
			kind = StreamKindMCPResourceCatalogRefreshFailed
		case "prompt_catalog_refreshed":
			kind = StreamKindMCPPromptCatalogRefreshed
		case "prompt_catalog_refresh_failed":
			kind = StreamKindMCPPromptCatalogRefreshFailed
		case "auth_status_changed":
			kind = StreamKindMCPAuthStatusChanged
		default:
			f.recordEventError(runID, fmt.Errorf("unknown MCP provider event %q", ev.Kind))
			return
		}

		payload := MCPProviderLifecyclePayload{
			ProviderName: ev.Provider,
			Transport:    ev.Transport,
			Error:        ev.Error,
			AuthStatus:   ev.AuthStatus,
		}

		if _, err := AppendStreamItem(context.Background(), f.deps.Store, sink, StreamItem{
			RunID:     runID,
			Kind:      kind,
			CreatedAt: time.Now().UTC(),
			Payload:   &payload,
		}); err != nil {
			f.recordEventError(runID, fmt.Errorf("append MCP lifecycle stream item %s: %w", kind, err))
		}
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
	if f.deps.IndexStore != nil {
		closeErr = errors.Join(closeErr, f.deps.IndexStore.Close())
		f.deps.IndexStore = nil
		f.deps.Crystallizer = nil
	}
	return closeErr
}
