package mcp

import (
	"context"
	"fmt"
	"log/slog"

	einotool "github.com/cloudwego/eino/components/tool"
)

// registerProviderTools registers every discovered tool for the named provider
// into the unified ToolRegistry, all or none: a tool whose spec cannot be
// built or registered unregisters the provider's tools and the error is
// returned, so the provider fails instead of losing a tool unseen. It is a
// no-op when no registry (or no spec builder) was wired, which only happens
// in tests.
//
// The slot is looked up by name under the manager read-lock; its tools are
// read outside the lock to avoid holding it across the (possibly slow) Info
// calls the spec builder performs. registeredToolNames records the namespaced
// names actually registered so unregisterProviderTools can remove exactly those.
func (m *Manager) registerProviderTools(ctx context.Context, providerName string) error {
	if m.toolRegistry == nil || m.specBuilder == nil {
		return nil
	}

	m.mu.RLock()
	slotIdx := -1
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName {
			slotIdx = i
			break
		}
	}
	if slotIdx < 0 {
		m.mu.RUnlock()
		return nil
	}
	tools := m.slots[slotIdx].tools()
	m.mu.RUnlock()

	for _, tool := range tools {
		spec, err := m.specBuilder(ctx, providerName, tool)
		if err == nil {
			err = m.toolRegistry.Register(spec)
		}
		if err != nil {
			m.unregisterProviderTools(providerName)
			return fmt.Errorf("register tools of MCP provider %s: %w", providerName, err)
		}
		// Record each name under the write lock as soon as it is registered,
		// so a concurrent Close sees it and unregisters it.
		m.mu.Lock()
		for i := range m.slots {
			if m.slots[i].cfg.Name == providerName {
				m.slots[i].registeredToolNames = append(m.slots[i].registeredToolNames, spec.Name)
				break
			}
		}
		m.mu.Unlock()
	}
	return nil
}

// unregisterProviderTools removes the specs previously registered for the named
// provider. It is a no-op when no registry was wired or the slot has no
// recorded names. Unknown-name errors from Unregister are logged and swallowed
// so a partial registration state never blocks cleanup of the remaining slots.
func (m *Manager) unregisterProviderTools(providerName string) {
	if m == nil || m.toolRegistry == nil {
		return
	}

	m.mu.Lock()
	slotIdx := -1
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName {
			slotIdx = i
			break
		}
	}
	var names []string
	if slotIdx >= 0 {
		names = m.slots[slotIdx].registeredToolNames
		m.slots[slotIdx].registeredToolNames = nil
	}
	m.mu.Unlock()

	for _, name := range names {
		if err := m.toolRegistry.Unregister(name); err != nil {
			slog.Warn("MCP tool unregister from unified registry skipped",
				"provider", providerName, "tool", name, "error", err)
		}
	}
}

// refreshProviderToolRegistrations re-syncs a provider's registry entries after
// its tool list changes (tools/list_changed notification). It unregisters the
// stale names then registers the current set; when that fails the provider
// is left with no registered tools.
func (m *Manager) refreshProviderToolRegistrations(ctx context.Context, providerName string) error {
	m.unregisterProviderTools(providerName)
	return m.registerProviderTools(ctx, providerName)
}

// tools returns the provider's discovered tools, or nil when the slot has no
// live provider.
func (s *providerSlot) tools() []einotool.BaseTool {
	if s == nil || s.p == nil {
		return nil
	}
	return s.p.tools
}
