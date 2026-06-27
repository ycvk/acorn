package config

import (
	"errors"
	"fmt"
	"strings"
)

func (c *Config) ValidateBase() error {
	if strings.TrimSpace(c.Agent.Name) == "" {
		return errors.New("runtime.name is required")
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
		case "readonly", "read_only", "serial":
		default:
			return fmt.Errorf("mcp.providers[%s].tool_safety must be one of readonly|read_only|serial, got %q", name, safety)
		}
	}
	if c.Memory.Search.MemoryContextTokenBudget <= 0 {
		c.Memory.Search.MemoryContextTokenBudget = 8000
	}
	if err := c.validateMemoryEmbedding(); err != nil {
		return err
	}
	c.validateMemoryReview()
	return nil
}

func (c *Config) ValidateExecutionReady() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	if c.Agent.MaxIterations <= 0 {
		return errors.New("runtime.max_iterations must be > 0")
	}
	if strings.TrimSpace(c.Tools.Workspace.RootDir) == "" {
		return errors.New("toolset.workspace.root_dir is required")
	}
	if !c.Tools.RunCommand.Disabled && strings.TrimSpace(c.Tools.RunCommand.WorkDir) == "" {
		return errors.New("toolset.run_command.work_dir is required when run_command is not disabled")
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
	return nil
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
	if c.Context.MaskAfterTurns < 0 {
		return errors.New("context.mask_after_turns must be >= 0")
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

// validateMemoryEmbedding validates the embedding config. When enabled, the
// model and dimensions must be set; defaults are applied if empty. When
// disabled, no validation is performed (the feature is inert).
func (c *Config) validateMemoryEmbedding() error {
	if !c.Memory.Embedding.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Memory.Embedding.Model) == "" {
		c.Memory.Embedding.Model = "text-embedding-3-small"
	}
	if c.Memory.Embedding.Dimensions <= 0 {
		c.Memory.Embedding.Dimensions = 1536
	}
	return nil
}

// validateMemoryReview validates the periodic review config. Defaults are
// applied when fields are zero. A zero ReviewInterval disables review.
func (c *Config) validateMemoryReview() {
	if c.Memory.Review.ReviewInterval < 0 {
		c.Memory.Review.ReviewInterval = 0
	}
	if c.Memory.Active.CharLimit <= 0 {
		c.Memory.Active.CharLimit = 2200
	}
}
