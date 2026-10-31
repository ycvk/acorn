package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// validateMCP checks the enabled MCP providers: unique names, a supported
// transport with its command or URL, a startup timeout and the auth type.
func (c *Config) validateMCP() error {
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
			if provider.Transport != "streamable_http" {
				return fmt.Errorf("mcp.providers[%s]: auth.type %q is only valid for the streamable_http transport", name, "oauth")
			}
		default:
			return fmt.Errorf("mcp.providers[%s]: auth.type must be one of none, oauth, got %q", name, authType)
		}
	}
	return nil
}

// validateSSEURL rejects SSE endpoints whose URL path contains a non-trivial
// prefix (more than one non-empty path segment after cleaning). Prefixed SSE
// deployments are known to misroute requests (go-sdk#687); operators should use
// streamable_http instead.
func validateSSEURL(providerName, rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("mcp.providers[%s].url is invalid: %w", providerName, err)
	}
	cleaned := filepath.Clean(parsed.Path)
	if cleaned == "" || cleaned == "/" {
		return nil
	}
	segments := strings.Split(strings.Trim(cleaned, "/"), "/")
	nonEmpty := 0
	for _, segment := range segments {
		if strings.TrimSpace(segment) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 1 {
		return fmt.Errorf("mcp.providers[%s]: SSE transport with path-prefix is not supported (path %q has multiple segments); use streamable_http transport instead", providerName, parsed.Path)
	}
	return nil
}
