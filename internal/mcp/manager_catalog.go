package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/core"
)

func (m *Manager) RefreshProviderCatalog(ctx context.Context, providerName string) error {
	if m == nil {
		return errors.New("manager is nil")
	}

	session, cfg, err := m.liveSession(providerName, "catalog refresh")
	if err != nil {
		return err
	}

	refreshCtx, cancel := context.WithTimeout(ctx, timeoutDuration(cfg.StartupTimeoutSeconds))
	defer cancel()

	rawTools, err := officialmcp.GetTools(refreshCtx, &officialmcp.Config{
		Cli:          session,
		ToolNameList: cfg.ToolNames,
	})
	if err != nil {
		return fmt.Errorf("refresh tool catalog for provider %q: %w", providerName, err)
	}

	toolNames, err := collectToolNames(refreshCtx, rawTools)
	if err != nil {
		return fmt.Errorf("collect tool names after refresh for provider %q: %w", providerName, err)
	}

	m.updateProvider(providerName, func(p *provider) {
		p.tools = rawTools
		p.toolNames = toolNames
	})

	// Re-sync the provider's registry entries so the unified ToolRegistry
	// reflects the refreshed tool list. No-op when no registry was wired.
	return m.refreshProviderToolRegistrations(ctx, providerName)
}

func (m *Manager) refreshProviderCatalogByType(ctx context.Context, providerName, catalogType string) error {
	if m == nil {
		return errors.New("manager is nil")
	}

	session, cfg, err := m.liveSession(providerName, catalogType+" catalog refresh")
	if err != nil {
		return err
	}

	refreshCtx, cancel := context.WithTimeout(ctx, timeoutDuration(cfg.StartupTimeoutSeconds))
	defer cancel()

	switch catalogType {
	case "resources":
		resResult, err := session.ListResources(refreshCtx, nil)
		if err != nil {
			return fmt.Errorf("refresh resource catalog for provider %q: %w", providerName, err)
		}
		m.updateProvider(providerName, func(p *provider) { p.resources = resResult.Resources })
	case "prompts":
		promptResult, err := session.ListPrompts(refreshCtx, nil)
		if err != nil {
			return fmt.Errorf("refresh prompt catalog for provider %q: %w", providerName, err)
		}
		m.updateProvider(providerName, func(p *provider) { p.prompts = promptResult.Prompts })
	default:
		return fmt.Errorf("unknown catalog type %q", catalogType)
	}

	return nil
}

// liveSession returns the session and config of a connected provider; what
// names the operation in the errors.
func (m *Manager) liveSession(providerName, what string) (*mcp.ClientSession, core.ProviderConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.slots {
		if m.slots[i].cfg.Name != providerName {
			continue
		}
		if p := m.slots[i].p; p == nil || p.session == nil {
			return nil, core.ProviderConfig{}, fmt.Errorf("provider %q has no live session for %s", providerName, what)
		}
		return m.slots[i].p.session, m.slots[i].cfg, nil
	}
	return nil, core.ProviderConfig{}, fmt.Errorf("provider %q not found for %s", providerName, what)
}

// updateProvider applies update to a provider's connection while it has one.
func (m *Manager) updateProvider(providerName string, update func(*provider)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName && m.slots[i].p != nil {
			update(m.slots[i].p)
			return
		}
	}
}

func (m *Manager) ResourceTools() []einotool.BaseTool {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tools []einotool.BaseTool
	for i := range m.slots {
		if m.slots[i].p != nil && m.slots[i].p.session != nil && len(m.slots[i].p.resources) > 0 {
			tools = append(tools, buildResourceTools(m.slots[i].p.session, m.slots[i].cfg.Name)...)
		}
	}
	return tools
}

func (m *Manager) PromptTools() []einotool.BaseTool {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tools []einotool.BaseTool
	for i := range m.slots {
		if m.slots[i].p != nil && m.slots[i].p.session != nil && len(m.slots[i].p.prompts) > 0 {
			tools = append(tools, buildPromptTools(m.slots[i].p.session, m.slots[i].cfg.Name)...)
		}
	}
	return tools
}

func Doctor(ctx context.Context, cfgs []core.ProviderConfig) []core.ProviderInfo {
	statuses := make([]core.ProviderInfo, 0, len(cfgs))
	for _, cfg := range cfgs {
		status := newProviderStatus(cfg)
		if !cfg.Enabled {
			statuses = append(statuses, status)
			continue
		}

		p, err := connectProvider(ctx, cfg, nil, nil)
		if err != nil {
			status.StartupStatus = "failed"
			status.Error = err.Error()
			statuses = append(statuses, status)
			continue
		}
		status.StartupStatus = "healthy"
		status.CommandPath = p.commandPath
		status.DiscoveredToolNames = append([]string(nil), p.toolNames...)
		status.ToolCount = len(p.toolNames)
		if err := p.close(); err != nil {
			slog.Warn("failed to close MCP provider after doctor check", "provider", cfg.Name, "error", err)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func newProviderStatus(cfg core.ProviderConfig) core.ProviderInfo {
	transport := NormalizeProviderTransport(cfg.Transport)
	authStatus := "none"
	if transport == "stdio" {
		authStatus = "env"
	}
	return core.ProviderInfo{
		Name:                cfg.Name,
		Configured:          true,
		Enabled:             cfg.Enabled,
		Transport:           transport,
		Command:             cfg.Command,
		Args:                append([]string(nil), cfg.Args...),
		WorkDir:             cfg.WorkDir,
		ConfiguredToolNames: append([]string(nil), cfg.ToolNames...),
		AuthStatus:          authStatus,
	}
}
