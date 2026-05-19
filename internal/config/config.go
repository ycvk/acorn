package config

type Config struct {
	Providers []ProviderConfig `yaml:"providers"`
	Context   ContextConfig    `yaml:"context"`
	Runtime   RuntimeConfig    `yaml:"runtime"`
	Web       WebConfig        `yaml:"web"`
	Agent     AgentConfig      `yaml:"agent"`
	Tools     ToolsConfig      `yaml:"tools"`
	MCP       MCPConfig        `yaml:"mcp"`
	Memory    MemoryConfig     `yaml:"memory"`
	Serve     ServeConfig      `yaml:"serve"`

	ConfigPath string `yaml:"-"`
	ConfigDir  string `yaml:"-"`
}

type ProviderConfig struct {
	Name                string         `yaml:"name"`
	Model               string         `yaml:"model"`
	BaseURL             string         `yaml:"base_url"`
	APIKey              string         `yaml:"api_key"`
	TimeoutSeconds      int            `yaml:"timeout_seconds"`
	Temperature         float32        `yaml:"temperature"`
	MaxCompletionTokens int            `yaml:"max_completion_tokens"`
	ReasoningEffort     string         `yaml:"reasoning_effort,omitempty"`
	ExtraFields         map[string]any `yaml:"extra_fields,omitempty"`
	Enabled             bool           `yaml:"enabled"`
}

type ContextConfig struct {
	WindowTokens        int `yaml:"window_tokens"`
	CompactMarginTokens int `yaml:"compact_margin_tokens"`
	PreserveRecentTurns int `yaml:"preserve_recent_turns"`
	SummaryMaxTokens    int `yaml:"summary_max_tokens"`
}

type ContextPolicy struct {
	ContextWindowTokens     int
	ReservedOutputTokens    int
	StaticOverheadTokens    int
	WarningBufferTokens     int
	AutoCompactBufferTokens int
	BlockingBufferTokens    int
	PreserveRecentTurns     int
	MaxSummaryTokens        int
	TokenEncoding           string
	HandoffFrameDisabled    bool
}

type MemoryConfig struct {
	Search   LayeredMemorySearchConfig `yaml:"search"`
	Semantic MemorySemanticConfig      `yaml:"semantic"`
}

type LayeredMemorySearchConfig struct {
	TokenBudget        int `yaml:"token_budget"`
	IndexTokenBudget   int `yaml:"index_token_budget"`
	InitialTokenBudget int `yaml:"initial_token_budget"`
	OnDemandReserve    int `yaml:"on_demand_reserve"`
}

type MemorySemanticConfig struct {
	Bleve     BleveSemanticConfig     `yaml:"bleve"`
	Embedding EmbeddingProviderConfig `yaml:"embedding"`
}

type BleveSemanticConfig struct {
	Path      string `yaml:"path"`
	IndexName string `yaml:"index_name"`
}

type EmbeddingProviderConfig struct {
	Provider       string `yaml:"provider"`
	Model          string `yaml:"model"`
	BaseURL        string `yaml:"base_url"`
	APIKey         string `yaml:"api_key"`
	Dimensions     int    `yaml:"dimensions"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	BatchSize      int    `yaml:"batch_size"`
}

type RuntimeConfig struct {
	StorageDir        string `yaml:"storage_dir"`
	RunTimeoutSeconds int    `yaml:"run_timeout_seconds"`
}

type WebConfig struct {
	ListenAddr     string   `yaml:"listen_addr"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type AgentConfig struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	SystemPrompt  string `yaml:"system_prompt"`
	MaxIterations int    `yaml:"max_iterations"`
}

type ToolsConfig struct {
	Workspace  WorkspaceToolConfig  `yaml:"workspace"`
	Mutation   MutationToolConfig   `yaml:"mutation"`
	RunCommand RunCommandToolConfig `yaml:"run_command"`
}

type WorkspaceToolConfig struct {
	RootDir string `yaml:"root_dir"`
}

type MutationToolConfig struct {
	Disabled bool     `yaml:"disabled,omitempty"`
	RootDir  string   `yaml:"root_dir,omitempty"`
	Denylist []string `yaml:"denylist"`
}

type RunCommandToolConfig struct {
	Disabled       bool     `yaml:"disabled,omitempty"`
	DefaultTimeout int      `yaml:"default_timeout"`
	WorkDir        string   `yaml:"work_dir"`
	EnvWhitelist   []string `yaml:"env_whitelist"`
}

type MCPConfig struct {
	Providers []MCPProviderConfig `yaml:"providers"`
}

// ServeConfig configures the MCP server mode for Acorn. When serve.tools.allowlist
// is non-empty, Acorn exposes a curated subset of its tools to external MCP clients
// via StreamableHTTP on the /mcp/* path.
type ServeConfig struct {
	Tools ServeToolsConfig `yaml:"tools"`
}

// ServeToolsConfig defines which tools the MCP server exposes.
type ServeToolsConfig struct {
	Allowlist []string `yaml:"allowlist"`
}

type MCPAuthConfig struct {
	Type       string   `yaml:"type"` // "none" | "oauth" | "api_key"
	ClientID   string   `yaml:"client_id"`
	Scopes     []string `yaml:"scopes"`
	TokenStore string   `yaml:"token_store"` // reserved for future use
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
	ToolSafety            string            `yaml:"tool_safety"`
}
