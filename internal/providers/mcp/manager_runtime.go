package mcpprovider

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (m *Manager) updateProviderAuthStatus(providerName, newStatus string) {
	m.mu.Lock()
	var oldStatus string
	for i := range m.slots {
		if m.slots[i].cfg.Name == providerName {
			oldStatus = m.slots[i].authStatus
			if oldStatus == newStatus {
				m.mu.Unlock()
				return
			}
			m.slots[i].authStatus = newStatus
			break
		}
	}
	m.mu.Unlock()
}

func (m *Manager) buildToolListChangedHandler(providerName string) func(context.Context, *mcp.ToolListChangedRequest) {
	return func(ctx context.Context, _ *mcp.ToolListChangedRequest) {
		if err := m.RefreshProviderCatalog(ctx, providerName); err != nil {
			slog.Warn("tool catalog refresh failed after tools/list_changed notification",
				"provider", providerName, "error", err)
		}
	}
}

func (m *Manager) buildResourceListChangedHandler(providerName string) func(context.Context, *mcp.ResourceListChangedRequest) {
	return func(ctx context.Context, _ *mcp.ResourceListChangedRequest) {
		if err := m.refreshProviderCatalogByType(ctx, providerName, "resources"); err != nil {
			slog.Warn("resource catalog refresh failed after resources/list_changed notification",
				"provider", providerName, "error", err)
		}
	}
}

func (m *Manager) buildPromptListChangedHandler(providerName string) func(context.Context, *mcp.PromptListChangedRequest) {
	return func(ctx context.Context, _ *mcp.PromptListChangedRequest) {
		if err := m.refreshProviderCatalogByType(ctx, providerName, "prompts"); err != nil {
			slog.Warn("prompt catalog refresh failed after prompts/list_changed notification",
				"provider", providerName, "error", err)
		}
	}
}

func (m *Manager) SetActiveRunID(runID string) {
	if m.elicitation != nil {
		m.elicitation.setActiveRunID(runID)
	}
}

func (m *Manager) SetSamplingExecutor(exec SamplingExecutor) {
	if m.sampling != nil {
		m.sampling.executor = exec
	}
}

func (m *Manager) buildElicitationHandler() func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	if m.elicitation == nil {
		return nil
	}
	return m.elicitation.HandleElicitation
}

func (m *Manager) buildCreateMessageHandler() func(context.Context, *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	if m.sampling == nil {
		return nil
	}
	return m.sampling.HandleCreateMessage
}
