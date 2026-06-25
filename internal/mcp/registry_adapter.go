package mcp

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// Compile-time assertion that *Manager satisfies core.ProviderRegistry.
var _ core.ProviderRegistry = (*Manager)(nil)

// RegisterProvider connects a single provider described by a core.ProviderConfig.
func (m *Manager) RegisterProvider(config core.ProviderConfig) error {
	if m == nil {
		return fmt.Errorf("manager is nil")
	}
	return m.connectSlotForReconcile(context.Background(), config)
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
func (m *Manager) GetProvider(name string) (core.ProviderInfo, bool) {
	if m == nil {
		return core.ProviderInfo{}, false
	}
	for _, st := range m.Statuses() {
		if st.Name == name {
			return st, true
		}
	}
	return core.ProviderInfo{}, false
}

// ListProviders returns status snapshots for all configured providers.
func (m *Manager) ListProviders() []core.ProviderInfo {
	return m.Statuses()
}

// Reconcile applies a new provider config set to the live manager.
func (m *Manager) Reconcile(ctx context.Context, configs []core.ProviderConfig) error {
	if m == nil {
		return fmt.Errorf("manager is nil")
	}
	return m.ReconcileProviders(ctx, configs)
}
