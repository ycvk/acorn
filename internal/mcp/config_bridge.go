package mcp

import (
	"reflect"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
)

// ProviderConfigsFromConfig normalizes config-layer MCP provider definitions
// into runtime provider configs so app/runtime paths share one translation.
func ProviderConfigsFromConfig(items []config.MCPProviderConfig) []core.ProviderConfig {
	providers := make([]core.ProviderConfig, 0, len(items))
	for _, item := range items {
		providers = append(providers, providerConfigFromAppConfig(item))
	}
	return providers
}

func providerConfigFromAppConfig(item config.MCPProviderConfig) core.ProviderConfig {
	return core.ProviderConfig{
		Name:                  item.Name,
		Enabled:               item.Enabled,
		Transport:             item.Transport,
		URL:                   item.URL,
		TimeoutSeconds:        item.TimeoutSeconds,
		Command:               item.Command,
		Args:                  item.Args,
		WorkDir:               item.WorkDir,
		Env:                   item.Env,
		ToolNames:             item.ToolNames,
		StartupTimeoutSeconds: item.StartupTimeoutSeconds,
		Auth: core.AuthConfig{
			Type:     item.Auth.Type,
			ClientID: item.Auth.ClientID,
			Scopes:   item.Auth.Scopes,
		},
	}
}

func providerConfigEquivalent(a, b core.ProviderConfig) bool {
	return reflect.DeepEqual(a, b)
}
