package mcp

import (
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/core"
)

// Test-only inspection helpers. Production consumes MCP tools through the
// unified ToolRegistry (ToolSpecBuilder at connect time) and ResourceTools /
// PromptTools; these accessors only expose manager state to tests.

type ResourceRegistration struct {
	ProviderName string
	Resources    []*mcp.Resource
	Session      *mcp.ClientSession
}

type PromptRegistration struct {
	ProviderName string
	Prompts      []*mcp.Prompt
	Session      *mcp.ClientSession
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

func (m *Manager) Statuses() []core.ProviderInfo {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]core.ProviderInfo, 0, len(m.slots))
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
