package mcpprovider

import "github.com/ycvk/acorn/internal/config"

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
		Env:                   cloneStringMap(item.Env),
		ToolNames:             append([]string(nil), item.ToolNames...),
		StartupTimeoutSeconds: item.StartupTimeoutSeconds,
		Auth: AuthConfig{
			Type:       item.Auth.Type,
			ClientID:   item.Auth.ClientID,
			Scopes:     append([]string(nil), item.Auth.Scopes...),
			TokenStore: item.Auth.TokenStore,
		},
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func providerConfigEquivalent(a, b ProviderConfig) bool {
	if a.Name != b.Name || a.Enabled != b.Enabled || a.Transport != b.Transport || a.URL != b.URL {
		return false
	}
	if a.TimeoutSeconds != b.TimeoutSeconds || a.Command != b.Command || a.WorkDir != b.WorkDir {
		return false
	}
	if a.StartupTimeoutSeconds != b.StartupTimeoutSeconds {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	if len(a.ToolNames) != len(b.ToolNames) {
		return false
	}
	for i := range a.ToolNames {
		if a.ToolNames[i] != b.ToolNames[i] {
			return false
		}
	}
	if len(a.Env) != len(b.Env) {
		return false
	}
	for k, v := range a.Env {
		if bv, ok := b.Env[k]; !ok || bv != v {
			return false
		}
	}
	if a.Auth.Type != b.Auth.Type {
		return false
	}
	if a.Auth.ClientID != b.Auth.ClientID {
		return false
	}
	if len(a.Auth.Scopes) != len(b.Auth.Scopes) {
		return false
	}
	for i := range a.Auth.Scopes {
		if a.Auth.Scopes[i] != b.Auth.Scopes[i] {
			return false
		}
	}
	return true
}
