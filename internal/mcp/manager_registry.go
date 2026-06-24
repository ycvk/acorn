package mcp

import (
	"context"
	"log/slog"

	einotool "github.com/cloudwego/eino/components/tool"
)

// registerProviderTools registers every discovered tool for the named provider
// into the unified ToolRegistry. It is a no-op when no registry (or no spec
// builder) was wired, so the legacy Manager-only path used by tests is
// unaffected.
//
// The slot is looked up by name under the manager read-lock; its tools are
// read outside the lock to avoid holding it across the (possibly slow) Info
// calls the spec builder performs. registeredToolNames records the namespaced
// names actually registered so unregisterProviderTools can remove exactly those.
//
// A tool whose spec builder returns an error, or whose Register returns an
// error (e.g. a duplicate name from another provider), is logged and skipped
// rather than aborting the whole provider: a single unruly tool should not
// poison an otherwise healthy connection. This mirrors the catalog's silent-omit
// behavior for absent backing services.
func (m *Manager) registerProviderTools(ctx context.Context, providerName string) {
	if m == nil || m.toolRegistry == nil || m.specBuilder == nil {
		return
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
		return
	}
	tools := m.slots[slotIdx].tools()
	m.mu.RUnlock()

	if len(tools) == 0 {
		return
	}

	registered := make([]string, 0, len(tools))
	for _, tool := range tools {
		spec, err := m.specBuilder(ctx, providerName, tool)
		if err != nil {
			slog.Warn("MCP tool spec build failed, skipping registration",
				"provider", providerName, "error", err)
			continue
		}
		if err := m.toolRegistry.Register(spec); err != nil {
			// A duplicate name from a different provider is the expected
			// collision case for cross-provider tools that share a suffix;
			// the registry rejects it and we log rather than fail.
			slog.Warn("MCP tool registration to unified registry skipped",
				"provider", providerName, "tool", spec.Name, "error", err)
			continue
		}
		registered = append(registered, spec.Name)
	}

	if len(registered) == 0 {
		return
	}

	m.mu.Lock()
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName {
			m.slots[i].registeredToolNames = registered
			break
		}
	}
	m.mu.Unlock()
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
// stale names then registers the current set. Called by RefreshProviderCatalog
// only when a registry is wired.
func (m *Manager) refreshProviderToolRegistrations(ctx context.Context, providerName string) {
	if m == nil || m.toolRegistry == nil || m.specBuilder == nil {
		return
	}
	m.unregisterProviderTools(providerName)
	m.registerProviderTools(ctx, providerName)
}

// tools returns the provider's discovered tools, or nil when the slot has no
// live provider. Held by providerSlot so the registry helper can read tools
// without importing the provider type's private layout.
func (s *providerSlot) tools() []einotool.BaseTool {
	if s == nil || s.p == nil {
		return nil
	}
	return s.p.tools
}
