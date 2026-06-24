package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (m *Manager) RefreshProviderCatalog(ctx context.Context, providerName string) error {
	if m == nil {
		return errors.New("manager is nil")
	}

	m.mu.Lock()
	slotIdx := -1
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName {
			slotIdx = i
			break
		}
	}
	if slotIdx < 0 {
		m.mu.Unlock()
		return fmt.Errorf("provider %q not found for catalog refresh", providerName)
	}
	slot := &m.slots[slotIdx]
	if slot.p == nil || slot.p.session == nil {
		m.mu.Unlock()
		return fmt.Errorf("provider %q has no live session for catalog refresh", providerName)
	}
	session := slot.p.session
	cfg := slot.cfg
	m.mu.Unlock()

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

	m.mu.Lock()
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName && m.slots[i].p != nil {
			m.slots[i].p.tools = rawTools
			m.slots[i].p.toolNames = toolNames
			break
		}
	}
	m.mu.Unlock()

	return nil
}

func (m *Manager) refreshProviderCatalogByType(ctx context.Context, providerName, catalogType string) error {
	if m == nil {
		return errors.New("manager is nil")
	}

	m.mu.Lock()
	slotIdx := -1
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName {
			slotIdx = i
			break
		}
	}
	if slotIdx < 0 {
		m.mu.Unlock()
		return fmt.Errorf("provider %q not found for %s catalog refresh", providerName, catalogType)
	}
	slot := &m.slots[slotIdx]
	if slot.p == nil || slot.p.session == nil {
		m.mu.Unlock()
		return fmt.Errorf("provider %q has no live session for %s catalog refresh", providerName, catalogType)
	}
	session := slot.p.session
	cfg := slot.cfg
	m.mu.Unlock()

	refreshCtx, cancel := context.WithTimeout(ctx, timeoutDuration(cfg.StartupTimeoutSeconds))
	defer cancel()

	switch catalogType {
	case "resources":
		resResult, err := session.ListResources(refreshCtx, nil)
		if err != nil {
			return fmt.Errorf("refresh resource catalog for provider %q: %w", providerName, err)
		}
		m.mu.Lock()
		for i := range m.slots {
			if m.slots[i].cfg.Name == providerName && m.slots[i].p != nil {
				m.slots[i].p.resources = resResult.Resources
				break
			}
		}
		m.mu.Unlock()
	case "prompts":
		promptResult, err := session.ListPrompts(refreshCtx, nil)
		if err != nil {
			return fmt.Errorf("refresh prompt catalog for provider %q: %w", providerName, err)
		}
		m.mu.Lock()
		for i := range m.slots {
			if m.slots[i].cfg.Name == providerName && m.slots[i].p != nil {
				m.slots[i].p.prompts = promptResult.Prompts
				break
			}
		}
		m.mu.Unlock()
	default:
		return fmt.Errorf("unknown catalog type %q", catalogType)
	}

	return nil
}

func (m *Manager) Tools() []einotool.BaseTool {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	tools := make([]einotool.BaseTool, 0)
	for i := range m.slots {
		if m.slots[i].p != nil {
			tools = append(tools, m.slots[i].p.tools...)
		}
	}
	return tools
}

func (m *Manager) Registrations() []ToolRegistration {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]ToolRegistration, 0)
	for i := range m.slots {
		if m.slots[i].p != nil {
			for _, tool := range m.slots[i].p.tools {
				items = append(items, ToolRegistration{
					ProviderName: m.slots[i].cfg.Name,
					Tool:         tool,
				})
			}
		}
	}
	return items
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

func (m *Manager) Resources() []*mcp.Resource {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var resources []*mcp.Resource
	for i := range m.slots {
		if m.slots[i].p != nil {
			resources = append(resources, m.slots[i].p.resources...)
		}
	}
	return resources
}

func (m *Manager) Prompts() []*mcp.Prompt {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var prompts []*mcp.Prompt
	for i := range m.slots {
		if m.slots[i].p != nil {
			prompts = append(prompts, m.slots[i].p.prompts...)
		}
	}
	return prompts
}

func (m *Manager) ResourceRegistrations() []ResourceRegistration {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var regs []ResourceRegistration
	for i := range m.slots {
		if m.slots[i].p != nil && len(m.slots[i].p.resources) > 0 {
			regs = append(regs, ResourceRegistration{
				ProviderName: m.slots[i].cfg.Name,
				Resources:    m.slots[i].p.resources,
				Session:      m.slots[i].p.session,
			})
		}
	}
	return regs
}

func (m *Manager) PromptRegistrations() []PromptRegistration {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var regs []PromptRegistration
	for i := range m.slots {
		if m.slots[i].p != nil && len(m.slots[i].p.prompts) > 0 {
			regs = append(regs, PromptRegistration{
				ProviderName: m.slots[i].cfg.Name,
				Prompts:      m.slots[i].p.prompts,
				Session:      m.slots[i].p.session,
			})
		}
	}
	return regs
}

func (m *Manager) Statuses() []ProviderStatus {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]ProviderStatus, 0, len(m.slots))
	for i := range m.slots {
		status := newProviderStatus(m.slots[i].cfg)
		status.StartupStatus = m.slots[i].startupStatus
		if m.slots[i].authStatus != "" {
			status.AuthStatus = m.slots[i].authStatus
		}
		if m.slots[i].p != nil {
			status.CommandPath = m.slots[i].p.commandPath
			status.DiscoveredToolNames = append([]string(nil), m.slots[i].p.toolNames...)
			status.ToolCount = len(m.slots[i].p.toolNames)
		}
		if m.slots[i].lastErr != nil {
			status.Error = m.slots[i].lastErr.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func Doctor(ctx context.Context, cfgs []ProviderConfig) []ProviderStatus {
	statuses := make([]ProviderStatus, 0, len(cfgs))
	for _, cfg := range cfgs {
		status := newProviderStatus(cfg)
		if !cfg.Enabled {
			statuses = append(statuses, status)
			continue
		}

		p, err := connectProvider(ctx, cfg, nil, nil, nil)
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

func newProviderStatus(cfg ProviderConfig) ProviderStatus {
	transport := NormalizeProviderTransport(cfg.Transport)
	authStatus := "none"
	if transport == "stdio" {
		authStatus = "env"
	} else if cfg.Auth.Type == "oauth" {
		authStatus = "none"
	}
	return ProviderStatus{
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
