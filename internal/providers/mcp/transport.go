package mcpprovider

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/toolset"
)

// TransportMetadata carries resolved transport kind and connection-relevant
// metadata so downstream surfaces do not need to re-parse raw config.
type TransportMetadata struct {
	Kind string // "stdio", "sse", or "streamable_http"
}

// NormalizeProviderTransport returns the trimmed transport value for an MCP
// provider config. Empty transport stays empty so config validation can reject it.
func NormalizeProviderTransport(transport string) string {
	return strings.TrimSpace(transport)
}

// NewTransportWithStore builds the correct go-sdk transport object for the
// given provider config, with optional OAuth support via the token store.
// When cfg.Auth.Type is "oauth" and the transport is "streamable_http", the
// transport is wrapped with an OAuthHandler backed by the store.
//
// SSEClientTransport does not support OAuth in go-sdk v1.5.0 (no OAuthHandler
// field), so OAuth on SSE is rejected. Stdio+OAuth is also rejected per D-05.
func NewTransportWithStore(cfg ProviderConfig, store TokenStore, onAuthStatusChanged ...func(status string)) (transport mcp.Transport, cleanup func(), metadata TransportMetadata, err error) {
	transportKind := NormalizeProviderTransport(cfg.Transport)
	if transportKind == "" {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: transport is required", cfg.Name)
	}

	// OAuth wrapping: only supported on streamable_http transport
	if cfg.Auth.Type == "oauth" {
		if transportKind != "streamable_http" {
			return nil, nil, TransportMetadata{}, fmt.Errorf(
				"provider %s: OAuth authentication is only supported on streamable_http transport, not %q",
				cfg.Name, transportKind)
		}
		var authCallback func(status string)
		if len(onAuthStatusChanged) > 0 {
			authCallback = onAuthStatusChanged[0]
		}
		return buildOAuthTransport(cfg, store, authCallback)
	}

	switch transportKind {
	case "stdio":
		return buildStdioTransport(cfg)
	case "sse":
		return buildSSETransport(cfg)
	case "streamable_http":
		return buildStreamableHTTPTransport(cfg)
	default:
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: unsupported transport %q (must be stdio, sse, or streamable_http)", cfg.Name, cfg.Transport)
	}
}

func buildStdioTransport(cfg ProviderConfig) (mcp.Transport, func(), TransportMetadata, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: command is required for stdio transport", cfg.Name)
	}

	commandPath, err := resolveCommandPath(cfg.Command)
	if err != nil {
		return nil, nil, TransportMetadata{}, err
	}

	cmd := exec.Command(commandPath, cfg.Args...)
	toolset.ConfigureCommand(cmd)
	if strings.TrimSpace(cfg.WorkDir) != "" {
		cmd.Dir = cfg.WorkDir
	}
	cmd.Env = mergeEnv(os.Environ(), cfg.Env)

	stderrFile, err := openProviderLogFile(cfg.Name)
	if err != nil {
		return nil, nil, TransportMetadata{}, err
	}
	cmd.Stderr = stderrFile

	cleanup := func() {
		if err := stderrFile.Close(); err != nil {
			slog.Warn("failed to close MCP stderr log file", "error", err)
		}
	}

	return &mcp.CommandTransport{Command: cmd}, cleanup, TransportMetadata{Kind: "stdio"}, nil
}

func buildSSETransport(cfg ProviderConfig) (mcp.Transport, func(), TransportMetadata, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: url is required for sse transport", cfg.Name)
	}
	cleanup := func() {}
	return &mcp.SSEClientTransport{Endpoint: cfg.URL}, cleanup, TransportMetadata{Kind: "sse"}, nil
}

func buildStreamableHTTPTransport(cfg ProviderConfig) (mcp.Transport, func(), TransportMetadata, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: url is required for streamable_http transport", cfg.Name)
	}
	cleanup := func() {}
	// MaxRetries is set to -1 so that startup failures are visible immediately
	// instead of being hidden behind implicit reconnect attempts. Phase 47
	// adds explicit reconnection with backpressure.
	return &mcp.StreamableClientTransport{Endpoint: cfg.URL, MaxRetries: -1}, cleanup, TransportMetadata{Kind: "streamable_http"}, nil
}

// buildOAuthTransport creates a StreamableClientTransport with an OAuthHandler
// for providers configured with auth.type=oauth. The OAuthHandler is backed by
// the provided token store for token persistence.
func buildOAuthTransport(cfg ProviderConfig, store TokenStore, onAuthStatusChanged func(status string)) (mcp.Transport, func(), TransportMetadata, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: url is required for OAuth streamable_http transport", cfg.Name)
	}
	if strings.TrimSpace(cfg.Auth.ClientID) == "" {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: auth.client_id is required for OAuth authentication", cfg.Name)
	}
	if store == nil {
		return nil, nil, TransportMetadata{}, fmt.Errorf("provider %s: token store is required for OAuth authentication", cfg.Name)
	}

	oauthHandler, err := newPersistentOAuthHandler(store, cfg.Name, cfg.URL, cfg.Auth.ClientID, cfg.Auth.Scopes, onAuthStatusChanged)
	if err != nil {
		return nil, nil, TransportMetadata{}, err
	}

	cleanup := func() {}
	return &mcp.StreamableClientTransport{
		Endpoint:     cfg.URL,
		MaxRetries:   -1,
		OAuthHandler: oauthHandler,
	}, cleanup, TransportMetadata{Kind: "streamable_http"}, nil
}
