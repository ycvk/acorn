package core

// AuthConfig specifies the authentication strategy for an MCP provider.
type AuthConfig struct {
	Type     string // "none" | "oauth" | "api_key"
	ClientID string
	Scopes   []string
}

// ProviderConfig holds the full configuration for a single MCP provider,
// covering both stdio and HTTP/SSE transports.
type ProviderConfig struct {
	Name                  string
	Enabled               bool
	Transport             string
	URL                   string
	TimeoutSeconds        int
	Command               string
	Args                  []string
	WorkDir               string
	Env                   map[string]string
	ToolNames             []string
	StartupTimeoutSeconds int
	Auth                  AuthConfig
}

// ProviderInfo is the read-only status snapshot of a configured MCP provider.
// It is the renamed equivalent of the former mcpprovider.ProviderStatus.
type ProviderInfo struct {
	Name                string   `json:"name"`
	Configured          bool     `json:"configured"`
	Enabled             bool     `json:"enabled"`
	Transport           string   `json:"transport,omitempty"`
	StartupStatus       string   `json:"startup_status,omitempty"`
	Command             string   `json:"command"`
	Args                []string `json:"args,omitempty"`
	WorkDir             string   `json:"work_dir,omitempty"`
	CommandPath         string   `json:"command_path,omitempty"`
	ConfiguredToolNames []string `json:"configured_tool_names,omitempty"`
	DiscoveredToolNames []string `json:"discovered_tool_names,omitempty"`
	ToolCount           int      `json:"tool_count"`
	Error               string   `json:"error,omitempty"`
	AuthStatus          string   `json:"auth_status,omitempty"` // "authenticated", "expired", "none", "env"
}
