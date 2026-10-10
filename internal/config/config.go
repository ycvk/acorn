package config

type Config struct {
	Providers []ProviderConfig `yaml:"providers"`
	Context   ContextConfig    `yaml:"context"`
	Runtime   RuntimeConfig    `yaml:"runtime"`
	Web       WebConfig        `yaml:"web"`
	WebAccess WebAccessConfig  `yaml:"web_access"`
	Browser   BrowserConfig    `yaml:"browser"`
	Agent     AgentConfig      `yaml:"agent"`
	Tools     ToolsConfig      `yaml:"tools"`
	MCP       MCPConfig        `yaml:"mcp"`
	Approval  ApprovalConfig   `yaml:"approval"`
	Owner     OwnerConfig      `yaml:"owner"`
	Memory    MemoryConfig     `yaml:"memory"`
	Presence  PresenceConfig   `yaml:"presence"`
	Thinking  ThinkingConfig   `yaml:"thinking"`
	Wake      WakeConfig       `yaml:"wake"`
	Notify    NotifyConfig     `yaml:"notify"`
	Knowledge KnowledgeConfig  `yaml:"knowledge"`
	Watch     WatchConfig      `yaml:"watch"`
	Briefing  BriefingConfig   `yaml:"briefing"`

	ConfigPath string `yaml:"-"`
	ConfigDir  string `yaml:"-"`
}

// ApprovalConfig lists tool-name glob patterns (path.Match syntax) whose calls
// pause for owner approval before executing.
type ApprovalConfig struct {
	Require []string `yaml:"require"`
}

type ProviderConfig struct {
	Name               string         `yaml:"name"`
	API                string         `yaml:"api"`
	Model              string         `yaml:"model"`
	BaseURL            string         `yaml:"base_url"`
	APIKey             string         `yaml:"api_key"`
	TimeoutSeconds     int            `yaml:"timeout_seconds"`
	Temperature        *float32       `yaml:"temperature,omitempty"`
	MaxOutputTokens    *int           `yaml:"max_output_tokens,omitempty"`
	IdleTimeoutSeconds *int           `yaml:"idle_timeout_seconds,omitempty"`
	ReasoningEffort    string         `yaml:"reasoning_effort,omitempty"`
	ExtraFields        map[string]any `yaml:"extra_fields,omitempty"`
	Enabled            bool           `yaml:"enabled"`
}

type ContextConfig struct {
	WindowTokens        int `yaml:"window_tokens"`
	CompactMarginTokens int `yaml:"compact_margin_tokens"`
	// MaskAfterTurns is how many of the most recent tool-call rounds stay
	// verbatim when older tool results are cleared to save context.
	MaskAfterTurns int `yaml:"mask_after_turns"`
}

type RuntimeConfig struct {
	StorageDir        string `yaml:"storage_dir"`
	RunTimeoutSeconds int    `yaml:"run_timeout_seconds"`
}

type WebConfig struct {
	ListenAddr     string   `yaml:"listen_addr"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type WebAccessConfig struct {
	UserAgent            string          `yaml:"user_agent"`
	TimeoutSeconds       int             `yaml:"timeout_seconds"`
	MaxResponseBytes     int64           `yaml:"max_response_bytes"`
	AllowPrivateNetworks bool            `yaml:"allow_private_networks"`
	Search               WebSearchConfig `yaml:"search"`
}

type WebSearchConfig struct {
	Provider       string `yaml:"provider"`
	APIKey         string `yaml:"api_key"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	MaxResults     int    `yaml:"max_results"`
}

type BrowserConfig struct {
	ExecutablePath        string `yaml:"executable_path"`
	Headless              bool   `yaml:"headless"`
	DefaultTimeoutSeconds int    `yaml:"default_timeout_seconds"`
}

type AgentConfig struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	MaxIterations int    `yaml:"max_iterations"`
}

type ToolsConfig struct {
	Workspace WorkspaceToolConfig `yaml:"workspace"`
}

type WorkspaceToolConfig struct {
	RootDir string `yaml:"root_dir"`
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
