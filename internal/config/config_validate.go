package config

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
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
	if err := c.validatePresence(); err != nil {
		return err
	}
	if err := c.validateKnowledge(); err != nil {
		return err
	}
	if err := c.validateThinking(); err != nil {
		return err
	}
	if err := c.validateWatch(); err != nil {
		return err
	}
	for i, pattern := range c.Approval.Require {
		// ask_operator already waits on the owner; gating it would ask twice.
		matchesAskOperator, err := path.Match(pattern, "ask_operator")
		if err != nil {
			return fmt.Errorf("approval.require[%d] %q: %w", i, pattern, err)
		}
		if matchesAskOperator {
			return fmt.Errorf("approval.require[%d] %q must not match ask_operator", i, pattern)
		}
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
	}
	return nil
}

func (c *Config) ValidateExecutionReady() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	if c.Agent.MaxIterations <= 0 {
		return errors.New("runtime.max_iterations must be > 0")
	}
	if c.WorkspaceRoot() == "" {
		return errors.New("tools.workspace.root_dir is required")
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	if _, err := c.InputTokenBudget(); err != nil {
		return err
	}
	if err := c.validatePresenceBudget(); err != nil {
		return err
	}
	return nil
}

// WorkspaceRoot is the directory that holds the repo seed skills and the
// workspace skills; Load resolves it against the config directory.
func (c *Config) WorkspaceRoot() string {
	root := strings.TrimSpace(c.Tools.Workspace.RootDir)
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
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
	if c.Context.CompactMarginTokens >= c.Context.WindowTokens {
		return errors.New("context.compact_margin_tokens must be < context.window_tokens")
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
