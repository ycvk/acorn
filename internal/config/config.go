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
	Memory    MemoryConfig     `yaml:"memory"`
	Triggers  TriggersConfig   `yaml:"triggers"`

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
	MaskAfterTurns      int `yaml:"mask_after_turns"`
}

type MemoryConfig struct {
	Search    MemorySearchConfig    `yaml:"search"`
	Embedding MemoryEmbeddingConfig `yaml:"embedding"`
}

type MemorySearchConfig struct {
	MemoryContextTokenBudget int `yaml:"memory_context_token_budget"`
}

// MemoryEmbeddingConfig configures the embedding-backed semantic retrieval
// layer. When Enabled, memory records are embedded on write and searched via
// sqlite-vec KNN alongside keyword matching (RRF fusion). The embedding
// endpoint reuses the primary provider's base_url + api_key (OpenAI-compatible
// /v1/embeddings). When disabled, search falls back to keyword-only (the
// pre-existing path), so this is opt-in with zero behavioral change for
// existing deployments.
type MemoryEmbeddingConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
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
	ToolSafety            string            `yaml:"tool_safety"`
}

// TriggersConfig configures ambient agent trigger sources. Triggers live in
// the serve process and fire new runs when external events arrive.
type TriggersConfig struct {
	Webhooks []WebhookTriggerConfig `yaml:"webhooks"`
	Crons    []CronTriggerConfig    `yaml:"crons"`
	// DebounceMillis coalesces rapid fires of the same trigger within this
	// window into a single run (last input wins). Zero disables debounce.
	// Protects against webhook spam burning LLM tokens. Recommended: 2000.
	DebounceMillis int `yaml:"debounce_millis"`
	// DailyQuota caps the number of trigger-started runs per UTC day.
	// Fires over quota are silently dropped (warned in logs). Zero disables
	// the cap (default). Protects against a runaway webhook burning tokens.
	DailyQuota int `yaml:"daily_quota"`
}

// WebhookTriggerConfig configures a single webhook trigger.
type WebhookTriggerConfig struct {
	ID     string `yaml:"id"`
	Secret string `yaml:"secret"`
	Prompt string `yaml:"prompt"`
}

// CronTriggerConfig configures a single cron trigger. Schedule is a standard
// 5-field cron expression (min hour dom month dow). The trigger fires a new
// run with Prompt as input at each matching time.
type CronTriggerConfig struct {
	ID       string `yaml:"id"`
	Schedule string `yaml:"schedule"`
	Prompt   string `yaml:"prompt"`
}
