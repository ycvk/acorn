package config

import (
	"errors"
	"fmt"
	"strings"
)

func (c *Config) ValidateBase() error {
	if strings.TrimSpace(c.Agent.Name) == "" {
		return errors.New("agent.name is required")
	}
	if strings.TrimSpace(c.Runtime.StorageDir) == "" {
		return errors.New("runtime.storage_dir is required")
	}
	if c.Runtime.RunTimeoutSeconds < 0 {
		return errors.New("runtime.run_timeout_seconds must be >= 0")
	}
	if strings.TrimSpace(c.Web.ListenAddr) == "" {
		return errors.New("web.listen_addr is required")
	}
	if err := c.validateWebAccessBase(); err != nil {
		return err
	}
	if err := c.validateBrowserBase(); err != nil {
		return err
	}
	seenProviderNames := make(map[string]struct{}, len(c.MCP.Providers))
	for _, provider := range c.MCP.Providers {
		if !provider.Enabled {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			return errors.New("mcp.providers[].name is required when provider is enabled")
		}
		if _, ok := seenProviderNames[name]; ok {
			return fmt.Errorf("duplicate enabled MCP provider name %q", name)
		}
		seenProviderNames[name] = struct{}{}
		if provider.Transport == "" {
			return fmt.Errorf("mcp.providers[%s].transport is required", name)
		}
		switch provider.Transport {
		case "stdio":
			if strings.TrimSpace(provider.Command) == "" {
				return fmt.Errorf("mcp.providers[%s].command is required for stdio transport", name)
			}
			if strings.TrimSpace(provider.URL) != "" {
				return fmt.Errorf("mcp.providers[%s].url must be empty for stdio transport", name)
			}
		case "sse":
			if strings.TrimSpace(provider.URL) == "" {
				return fmt.Errorf("mcp.providers[%s].url is required for sse transport", name)
			}
			if err := validateSSEURL(name, provider.URL); err != nil {
				return err
			}
		case "streamable_http":
			if strings.TrimSpace(provider.URL) == "" {
				return fmt.Errorf("mcp.providers[%s].url is required for streamable_http transport", name)
			}
		default:
			return fmt.Errorf("mcp.providers[%s].transport must be one of stdio|sse|streamable_http", name)
		}
		if provider.StartupTimeoutSeconds <= 0 {
			return fmt.Errorf("mcp.providers[%s].startup_timeout_seconds must be > 0", name)
		}
		authType := strings.TrimSpace(provider.Auth.Type)
		switch authType {
		case "", "none":
			// default / valid
		case "oauth":
			if provider.Transport == "stdio" {
				return fmt.Errorf("mcp.providers[%s]: auth.type %q is only valid for sse and streamable_http transports", name, "oauth")
			}
		case "api_key":
			// valid
		default:
			return fmt.Errorf("mcp.providers[%s]: auth.type must be one of none, oauth, api_key, got %q", name, authType)
		}
		safety := strings.TrimSpace(provider.ToolSafety)
		if safety == "" {
			return fmt.Errorf("mcp.providers[%s].tool_safety is required", name)
		}
		switch safety {
		case "readonly", "read_only", "write_scoped", "never_parallel":
		default:
			return fmt.Errorf("mcp.providers[%s].tool_safety must be one of readonly|read_only|write_scoped|never_parallel, got %q", name, safety)
		}
	}
	if c.Memory.Search.MemoryContextTokenBudget <= 0 {
		c.Memory.Search.MemoryContextTokenBudget = 2000
	}
	if err := c.validateMemorySemanticBase(); err != nil {
		return err
	}
	// Validate serve.tools.allowlist -- reject duplicates and empty entries
	if len(c.Serve.Tools.Allowlist) > 0 {
		seen := make(map[string]bool, len(c.Serve.Tools.Allowlist))
		for _, name := range c.Serve.Tools.Allowlist {
			normalized := strings.TrimSpace(name)
			if normalized == "" {
				return errors.New("serve.tools.allowlist contains empty entry")
			}
			if seen[normalized] {
				return fmt.Errorf("serve.tools.allowlist contains duplicate %q", normalized)
			}
			seen[normalized] = true
		}
		c.Serve.Tools.Allowlist = normalizePermissionToolNames(c.Serve.Tools.Allowlist)
	}
	return nil
}

func normalizePermissionToolNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func (c *Config) ValidateExecutionReady() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	if c.Agent.MaxIterations <= 0 {
		return errors.New("agent.max_iterations must be > 0")
	}
	if strings.TrimSpace(c.Tools.Workspace.RootDir) == "" {
		return errors.New("tools.workspace.root_dir is required")
	}
	if !c.Tools.RunCommand.Disabled && strings.TrimSpace(c.Tools.RunCommand.WorkDir) == "" {
		return errors.New("tools.run_command.work_dir is required when run_command is not disabled")
	}
	if _, err := c.Workspace(); err != nil {
		return err
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	if err := c.validateContext(); err != nil {
		return err
	}
	// Semantic retrieval is an optional enhancement. Only enforce its full field
	// contract when the operator actually configured it; an unconfigured semantic
	// section must not block execution readiness (the run hot-path Prepare degrades
	// to an empty memory result instead). When configured, every field is still
	// validated so a half-configured semantic section fails loud.
	if c.MemorySemanticConfigured() {
		if err := c.validateMemorySemanticExecution(); err != nil {
			return err
		}
	}
	return nil
}

// MemorySemanticConfigured reports whether the operator intends to use semantic
// retrieval. Presence of an embedding model or base_url is the signal. When this
// is false, semantic retrieval is an unwired optional capability: it does not
// block execution readiness, and the run hot-path Prepare degrades to an empty
// memory result. Explicit Search/SearchSemantic callers still fail loud.
func (c *Config) MemorySemanticConfigured() bool {
	if c == nil {
		return false
	}
	embedding := c.Memory.Semantic.Embedding
	return strings.TrimSpace(embedding.Model) != "" || strings.TrimSpace(embedding.BaseURL) != ""
}

func (c *Config) ValidateMemorySemanticReady() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	if err := c.validateMemorySemanticExecution(); err != nil {
		return err
	}
	return nil
}

func (c *Config) Validate() error {
	return c.ValidateExecutionReady()
}

func (c *Config) WorkspaceRoot() string {
	ws, err := c.Workspace()
	if err != nil || ws == nil {
		return ""
	}
	return ws.Root()
}

func (c *Config) validateContext() error {
	if c == nil {
		return nil
	}
	if c.Context.WindowTokens <= 0 {
		return errors.New("context.window_tokens must be > 0")
	}
	if c.Context.CompactMarginTokens <= 1 {
		return errors.New("context.compact_margin_tokens must be > 1")
	}
	if c.Context.PreserveRecentTurns < 1 {
		return errors.New("context.preserve_recent_turns must be >= 1")
	}
	if c.Context.SummaryMaxTokens <= 0 {
		return errors.New("context.summary_max_tokens must be > 0")
	}
	policy, err := c.ContextPolicy()
	if err != nil {
		return err
	}
	reserved := max(policy.ReservedOutputTokens, policy.SummaryMaxTokens)
	effectiveWindow := policy.WindowTokens - reserved - defaultContextStaticOverheadTokens
	warningThreshold := policy.CompactMarginTokens + defaultContextWarningGapTokens
	if effectiveWindow <= warningThreshold {
		return errors.New("context effective window must be greater than derived warning threshold buffer")
	}
	return nil
}

func (c *Config) validateMemorySemanticBase() error {
	if c == nil {
		return nil
	}
	semantic := c.Memory.Semantic
	if provider := strings.TrimSpace(semantic.Embedding.Provider); provider != "" && provider != "openai_compatible" {
		return fmt.Errorf("memory.semantic.embedding.provider must be openai_compatible, got %q", c.Memory.Semantic.Embedding.Provider)
	}
	return nil
}

func (c *Config) validateMemorySemanticExecution() error {
	if c == nil {
		return nil
	}
	semantic := c.Memory.Semantic
	if strings.TrimSpace(semantic.Bleve.IndexName) == "" {
		return errors.New("memory.semantic.bleve.index_name is required")
	}
	if strings.TrimSpace(semantic.Embedding.Provider) == "" {
		return errors.New("memory.semantic.embedding.provider is required")
	}
	if strings.TrimSpace(semantic.Embedding.Model) == "" {
		return errors.New("memory.semantic.embedding.model is required")
	}
	if strings.TrimSpace(semantic.Embedding.BaseURL) == "" {
		return errors.New("memory.semantic.embedding.base_url is required")
	}
	if strings.TrimSpace(semantic.Embedding.APIKey) == "" {
		return errors.New("memory.semantic.embedding.api_key is required (semantic recall is configured because embedding.model/base_url are set) — set the key, or remove embedding.model+base_url to run without semantic recall")
	}
	if semantic.Embedding.Dimensions <= 0 {
		return errors.New("memory.semantic.embedding.dimensions must be > 0")
	}
	if semantic.Embedding.TimeoutSeconds <= 0 {
		return errors.New("memory.semantic.embedding.timeout_seconds must be > 0")
	}
	if semantic.Embedding.BatchSize <= 0 {
		return errors.New("memory.semantic.embedding.batch_size must be > 0")
	}
	return nil
}

func (c *Config) validateWebAccessBase() error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.WebAccess.UserAgent) == "" {
		return errors.New("web_access.user_agent is required")
	}
	if c.WebAccess.TimeoutSeconds <= 0 {
		return errors.New("web_access.timeout_seconds must be > 0")
	}
	if c.WebAccess.MaxResponseBytes <= 0 {
		return errors.New("web_access.max_response_bytes must be > 0")
	}
	search := c.WebAccess.Search
	switch strings.TrimSpace(search.Provider) {
	case "tavily":
	default:
		return fmt.Errorf("web_access.search.provider must be tavily, got %q", search.Provider)
	}
	if search.TimeoutSeconds <= 0 {
		return errors.New("web_access.search.timeout_seconds must be > 0")
	}
	if search.MaxResults <= 0 {
		return errors.New("web_access.search.max_results must be > 0")
	}
	return nil
}

func (c *Config) validateBrowserBase() error {
	if c == nil {
		return nil
	}
	if c.Browser.DefaultTimeoutSeconds <= 0 {
		return errors.New("browser.default_timeout_seconds must be > 0")
	}
	return nil
}
