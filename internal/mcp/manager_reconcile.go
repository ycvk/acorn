package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (m *Manager) ReconcileProviders(ctx context.Context, cfgs []ProviderConfig) error {
	if m == nil {
		return errors.New("manager is nil")
	}

	enabled := make([]ProviderConfig, 0, len(cfgs))
	for _, cfg := range cfgs {
		if cfg.Enabled {
			enabled = append(enabled, cfg)
		}
	}

	desired := make(map[string]ProviderConfig, len(enabled))
	for _, cfg := range enabled {
		desired[cfg.Name] = cfg
	}

	m.mu.Lock()
	type slotSnapshot struct {
		cfg       ProviderConfig
		unchanged bool
	}
	currentNames := make([]string, 0, len(m.slots))
	currentByName := make(map[string]slotSnapshot, len(m.slots))
	for _, slot := range m.slots {
		name := slot.cfg.Name
		currentNames = append(currentNames, name)
		d, inDesired := desired[name]
		unchanged := inDesired && providerConfigEquivalent(slot.cfg, d)
		currentByName[name] = slotSnapshot{cfg: slot.cfg, unchanged: unchanged}
	}
	m.mu.Unlock()

	var (
		toAdd     []ProviderConfig
		toRemove  []string
		toRestart []ProviderConfig
	)

	for _, name := range currentNames {
		snap := currentByName[name]
		if _, ok := desired[name]; !ok {
			toRemove = append(toRemove, name)
		} else if !snap.unchanged {
			toRestart = append(toRestart, desired[name])
		}
	}

	for _, cfg := range enabled {
		if _, exists := currentByName[cfg.Name]; !exists {
			toAdd = append(toAdd, cfg)
		}
	}

	for _, name := range toRemove {
		m.closeSlotByName(name)
	}

	for _, cfg := range toRestart {
		m.closeSlotByName(cfg.Name)
		if err := m.connectSlotForReconcile(ctx, cfg); err != nil {
			return fmt.Errorf("restart MCP provider %q: %w", cfg.Name, err)
		}
	}

	for _, cfg := range toAdd {
		if err := m.connectSlotForReconcile(ctx, cfg); err != nil {
			return fmt.Errorf("add MCP provider %q: %w", cfg.Name, err)
		}
	}

	m.rebuildSlotOrder(currentNames, toAdd)
	return nil
}

func (m *Manager) closeSlotByName(name string) {
	// Unregister the provider's tools from the unified registry before closing
	// the session. unregisterProviderTools reads registeredToolNames from the
	// slot under the lock and clears it, so this must run while the slot still
	// exists. It is a no-op when no registry was wired.
	m.unregisterProviderTools(name)

	m.mu.Lock()
	for i, slot := range m.slots {
		if slot.cfg.Name == name {
			if slot.p != nil {
				if err := slot.p.close(); err != nil {
					slog.Warn("close MCP provider during reconciliation", "provider", name, "error", err)
				}
			}
			m.slots = append(m.slots[:i], m.slots[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

func (m *Manager) connectSlotForReconcile(ctx context.Context, cfg ProviderConfig) error {
	clientOpts := &mcp.ClientOptions{
		ToolListChangedHandler:     m.buildToolListChangedHandler(cfg.Name),
		ResourceListChangedHandler: m.buildResourceListChangedHandler(cfg.Name),
		PromptListChangedHandler:   m.buildPromptListChangedHandler(cfg.Name),
		ElicitationHandler:         m.buildElicitationHandler(),
		CreateMessageHandler:       m.buildCreateMessageHandler(),
	}

	p, err := connectProviderFunc(ctx, cfg, clientOpts, m.tokenStore, func(status string) { m.updateProviderAuthStatus(cfg.Name, status) })

	m.mu.Lock()
	slot := providerSlot{
		cfg:        cfg,
		authStatus: newProviderStatus(cfg).AuthStatus,
	}
	if err != nil {
		slot.lastErr = err
		slot.startupStatus = "failed"
		m.slots = append(m.slots, slot)
		m.mu.Unlock()
		return err
	}

	slot.p = p
	slot.startupStatus = "healthy"
	m.slots = append(m.slots, slot)
	m.mu.Unlock()

	if cfg.Auth.Type == "oauth" {
		m.updateProviderAuthStatus(cfg.Name, "authenticated")
	}

	// Register the newly connected provider's tools into the unified registry.
	// No-op when no registry was wired (legacy/test path).
	m.registerProviderTools(ctx, cfg.Name)

	return nil
}

func (m *Manager) rebuildSlotOrder(existingOrder []string, added []ProviderConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	byName := make(map[string]providerSlot, len(m.slots))
	for _, slot := range m.slots {
		byName[slot.cfg.Name] = slot
	}

	ordered := make([]providerSlot, 0, len(m.slots))
	seen := make(map[string]bool, len(m.slots))
	for _, name := range existingOrder {
		if slot, ok := byName[name]; ok {
			ordered = append(ordered, slot)
			seen[name] = true
		}
	}
	for _, cfg := range added {
		if slot, ok := byName[cfg.Name]; ok && !seen[cfg.Name] {
			ordered = append(ordered, slot)
			seen[cfg.Name] = true
		}
	}
	m.slots = ordered
}
