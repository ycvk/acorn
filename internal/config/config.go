package config

// Config is what is set once at deployment: addresses, keys and limits the
// host's other services share. The owner's schedule, budgets and approval
// rules are settings in the database.
type Config struct {
	Providers   []ProviderConfig  `yaml:"providers"`
	Runtime     RuntimeConfig     `yaml:"runtime"`
	Web         WebConfig         `yaml:"web"`
	WebAccess   WebAccessConfig   `yaml:"web_access"`
	MCP         MCPConfig         `yaml:"mcp"`
	Memory      MemoryConfig      `yaml:"memory"`
	Notify      NotifyConfig      `yaml:"notify"`
	Watch       WatchConfig       `yaml:"watch"`
	Backup      BackupConfig      `yaml:"backup"`
	Workstation WorkstationConfig `yaml:"workstation"`
	Files       FilesConfig       `yaml:"files"`
	Review      ReviewConfig      `yaml:"review"`

	ConfigPath string `yaml:"-"`
	ConfigDir  string `yaml:"-"`
}

// BackupConfig is where acorn backup writes snapshots and how many it keeps.
// An empty Dir turns backups off.
type BackupConfig struct {
	Dir  string `yaml:"dir"`
	Keep int    `yaml:"keep"`
}

type ProviderConfig struct {
	Name            string   `yaml:"name"`
	API             string   `yaml:"api"`
	Model           string   `yaml:"model"`
	BaseURL         string   `yaml:"base_url"`
	APIKey          string   `yaml:"api_key"`
	TimeoutSeconds  int      `yaml:"timeout_seconds"`
	Temperature     *float32 `yaml:"temperature,omitempty"`
	MaxOutputTokens *int     `yaml:"max_output_tokens,omitempty"`
	// ContextWindowTokens is the context window of a model the model table
	// does not know; known models use their own.
	ContextWindowTokens *int           `yaml:"context_window_tokens,omitempty"`
	IdleTimeoutSeconds  *int           `yaml:"idle_timeout_seconds,omitempty"`
	ReasoningEffort     string         `yaml:"reasoning_effort,omitempty"`
	ExtraFields         map[string]any `yaml:"extra_fields,omitempty"`
	Enabled             bool           `yaml:"enabled"`
}

type RuntimeConfig struct {
	StorageDir string `yaml:"storage_dir"`
}

type WebConfig struct {
	ListenAddr     string   `yaml:"listen_addr"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type WebAccessConfig struct {
	AllowPrivateNetworks bool            `yaml:"allow_private_networks"`
	Search               WebSearchConfig `yaml:"search"`
}

type WebSearchConfig struct {
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
}

type MCPConfig struct {
	Providers []MCPProviderConfig `yaml:"providers"`
}

type MCPAuthConfig struct {
	Type     string   `yaml:"type"` // "none" | "oauth" | "api_key"
	ClientID string   `yaml:"client_id"`
	Scopes   []string `yaml:"scopes"`
}

type MCPProviderConfig struct {
	Enabled               bool              `yaml:"enabled"`
	Name                  string            `yaml:"name"`
	Transport             string            `yaml:"transport"`
	URL                   string            `yaml:"url"`
	TimeoutSeconds        int               `yaml:"timeout_seconds"`
	Command               string            `yaml:"command"`
	Args                  []string          `yaml:"args"`
	WorkDir               string            `yaml:"work_dir"`
	Env                   map[string]string `yaml:"env"`
	ToolNames             []string          `yaml:"tool_names"`
	StartupTimeoutSeconds int               `yaml:"startup_timeout_seconds"`
	Auth                  MCPAuthConfig     `yaml:"auth"`
}
