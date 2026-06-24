package mcp

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// Compile-time assertion that *Manager satisfies core.ProviderRegistry.
var _ core.ProviderRegistry = (*Manager)(nil)

// RegisterProvider connects a single provider described by a core.ProviderConfig.
// The core type is converted to the package-local mcp.ProviderConfig before
// being handed to the existing connection path.
func (m *Manager) RegisterProvider(config core.ProviderConfig) error {
	if m == nil {
		return fmt.Errorf("manager is nil")
	}
	return m.connectSlotForReconcile(context.Background(), providerConfigFromCore(config))
}

// UnregisterProvider disconnects and removes the named provider, if present.
func (m *Manager) UnregisterProvider(name string) error {
	if m == nil {
		return fmt.Errorf("manager is nil")
	}
	m.closeSlotByName(name)
	return nil
}

// GetProvider returns the status snapshot of a single provider by name.
// The boolean is false when no slot matches.
func (m *Manager) GetProvider(name string) (core.ProviderInfo, bool) {
	if m == nil {
		return core.ProviderInfo{}, false
	}
	for _, st := range m.Statuses() {
		if st.Name == name {
			return providerStatusToCore(st), true
		}
	}
	return core.ProviderInfo{}, false
}

// ListProviders returns status snapshots for all configured providers, converted
// to core.ProviderInfo.
func (m *Manager) ListProviders() []core.ProviderInfo {
	statuses := m.Statuses()
	infos := make([]core.ProviderInfo, 0, len(statuses))
	for _, st := range statuses {
		infos = append(infos, providerStatusToCore(st))
	}
	return infos
}

// providerConfigFromCore converts a core.ProviderConfig into the package-local
// mcp.ProviderConfig. The two types have identical fields; this function is the
// single explicit bridge so callers stay in core terms.
func providerConfigFromCore(c core.ProviderConfig) ProviderConfig {
	return ProviderConfig{
		Name:                  c.Name,
		Enabled:               c.Enabled,
		Transport:             c.Transport,
		URL:                   c.URL,
		TimeoutSeconds:        c.TimeoutSeconds,
		Command:               c.Command,
		Args:                  append([]string(nil), c.Args...),
		WorkDir:               c.WorkDir,
		Env:                   cloneStringMap(c.Env),
		ToolNames:             append([]string(nil), c.ToolNames...),
		StartupTimeoutSeconds: c.StartupTimeoutSeconds,
		Auth: AuthConfig{
			Type:     c.Auth.Type,
			ClientID: c.Auth.ClientID,
			Scopes:   append([]string(nil), c.Auth.Scopes...),
		},
	}
}

// providerStatusToCore converts the package-local ProviderStatus into a
// core.ProviderInfo. The two types have identical fields and JSON tags.
func providerStatusToCore(s ProviderStatus) core.ProviderInfo {
	return core.ProviderInfo{
		Name:                s.Name,
		Configured:          s.Configured,
		Enabled:             s.Enabled,
		Transport:           s.Transport,
		StartupStatus:       s.StartupStatus,
		Command:             s.Command,
		Args:                s.Args,
		WorkDir:             s.WorkDir,
		CommandPath:         s.CommandPath,
		ConfiguredToolNames: s.ConfiguredToolNames,
		DiscoveredToolNames: s.DiscoveredToolNames,
		ToolCount:           s.ToolCount,
		Error:               s.Error,
		AuthStatus:          s.AuthStatus,
	}
}
