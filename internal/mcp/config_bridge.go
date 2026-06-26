package mcp

import (
	"maps"
	"reflect"

	"github.com/ycvk/acorn/internal/config"
)

// ProviderConfigsFromConfig normalizes config-layer MCP provider definitions
// into runtime provider configs so app/runtime paths share one translation.
func ProviderConfigsFromConfig(items []config.MCPProviderConfig) []ProviderConfig {
	providers := make([]ProviderConfig, 0, len(items))
	for _, item := range items {
		providers = append(providers, providerConfigFromAppConfig(item))
	}
	return providers
}

func providerConfigFromAppConfig(item config.MCPProviderConfig) ProviderConfig {
	return ProviderConfig{
		Name:                  item.Name,
		Enabled:               item.Enabled,
		Transport:             item.Transport,
		URL:                   item.URL,
		TimeoutSeconds:        item.TimeoutSeconds,
		Command:               item.Command,
		Args:                  append([]string(nil), item.Args...),
		WorkDir:               item.WorkDir,
		Env:                   maps.Clone(item.Env),
		ToolNames:             append([]string(nil), item.ToolNames...),
		StartupTimeoutSeconds: item.StartupTimeoutSeconds,
		Auth: AuthConfig{
			Type:     item.Auth.Type,
			ClientID: item.Auth.ClientID,
			Scopes:   append([]string(nil), item.Auth.Scopes...),
		},
	}
}

func providerConfigEquivalent(a, b ProviderConfig) bool {
	return reflect.DeepEqual(a, b)
}
