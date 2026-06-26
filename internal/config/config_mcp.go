package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

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
