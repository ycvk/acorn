package mcp

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewTransportStdio(t *testing.T) {
	// Stdio transport requires a valid command; use "echo" as a minimal fixture.
	cfg := ProviderConfig{
		Name:                  "test_stdio",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "echo",
		Args:                  []string{},
		StartupTimeoutSeconds: 10,
	}
	transport, cleanup, metadata, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport stdio: %v", err)
	}
	defer cleanup()
	if _, ok := transport.(*mcp.CommandTransport); !ok {
		t.Fatalf("expected *mcp.CommandTransport, got %T", transport)
	}
	if got, want := metadata.Kind, "stdio"; got != want {
		t.Fatalf("metadata.Kind = %q, want %q", got, want)
	}
}

func TestNewTransportSSE(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_sse",
		Enabled:               true,
		Transport:             "sse",
		URL:                   "http://localhost:8080/sse",
		StartupTimeoutSeconds: 10,
	}
	transport, cleanup, metadata, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport sse: %v", err)
	}
	defer cleanup()
	sseTransport, ok := transport.(*mcp.SSEClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.SSEClientTransport, got %T", transport)
	}
	if got, want := sseTranscore.Endpoint, cfg.URL; got != want {
		t.Fatalf("SSEClientTranscore.Endpoint = %q, want %q", got, want)
	}
	if got, want := metadata.Kind, "sse"; got != want {
		t.Fatalf("metadata.Kind = %q, want %q", got, want)
	}
}

func TestNewTransportStreamableHTTP(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_streamable",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "http://localhost:8080/mcp",
		StartupTimeoutSeconds: 10,
	}
	transport, cleanup, metadata, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport streamable_http: %v", err)
	}
	defer cleanup()
	sHttp, ok := transport.(*mcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.StreamableClientTransport, got %T", transport)
	}
	if got, want := sHttp.Endpoint, cfg.URL; got != want {
		t.Fatalf("StreamableClientTranscore.Endpoint = %q, want %q", got, want)
	}
	if got, want := sHttp.MaxRetries, -1; got != want {
		t.Fatalf("StreamableClientTranscore.MaxRetries = %d, want %d", got, want)
	}
	if got, want := metadata.Kind, "streamable_http"; got != want {
		t.Fatalf("metadata.Kind = %q, want %q", got, want)
	}
}

func TestNewTransportEmptyTransportRejected(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_missing_transport",
		Enabled:               true,
		Transport:             "",
		Command:               "echo",
		StartupTimeoutSeconds: 10,
	}
	_, cleanup, _, err := NewTransportWithStore(cfg, nil)
	if err == nil {
		t.Fatal("expected empty transport to be rejected")
	}
	if cleanup != nil {
		t.Fatal("cleanup should be nil when transport creation fails")
	}
	if !strings.Contains(err.Error(), "transport is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewTransportInvalidTransport(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_bad",
		Enabled:               true,
		Transport:             "websocket",
		URL:                   "ws://localhost:8080",
		StartupTimeoutSeconds: 10,
	}
	_, _, _, err := NewTransportWithStore(cfg, nil)
	if err == nil {
		t.Fatal("expected unsupported transport to be rejected")
	}
}

func TestNewTransportStdioMissingCommand(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_no_cmd",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "",
		StartupTimeoutSeconds: 10,
	}
	_, _, _, err := NewTransportWithStore(cfg, nil)
	if err == nil {
		t.Fatal("expected stdio without command to be rejected")
	}
}

func TestNewTransportSSEMissingURL(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_no_url",
		Enabled:               true,
		Transport:             "sse",
		URL:                   "",
		StartupTimeoutSeconds: 10,
	}
	_, _, _, err := NewTransportWithStore(cfg, nil)
	if err == nil {
		t.Fatal("expected SSE without URL to be rejected")
	}
}

func TestNewTransportStreamableHTTPMissingURL(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "test_no_url",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "",
		StartupTimeoutSeconds: 10,
	}
	_, _, _, err := NewTransportWithStore(cfg, nil)
	if err == nil {
		t.Fatal("expected streamable_http without URL to be rejected")
	}
}

func TestNormalizeProviderTransportMCP(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  ", ""},
		{"stdio", "stdio"},
		{"sse", "sse"},
		{"streamable_http", "streamable_http"},
		{"unknown", "unknown"},
	}
	for _, tc := range cases {
		got := NormalizeProviderTransport(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeProviderTransport(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestBuildStdioTransportSetsStderrFile(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "stderr_test",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "echo",
		Args:                  []string{},
		StartupTimeoutSeconds: 10,
	}
	_, cleanup, _, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport stdio: %v", err)
	}
	// cleanup should close the stderr file; calling it twice should not panic
	cleanup()
	cleanup() // second call is a no-op for the file path
}

func TestBuildTransportMetadataFields(t *testing.T) {
	cases := []struct {
		name      string
		transport string
		url       string
		command   string
		wantKind  string
	}{
		{"stdio_meta", "stdio", "", "echo", "stdio"},
		{"sse_meta", "sse", "http://localhost:8080/sse", "", "sse"},
		{"streamable_meta", "streamable_http", "http://localhost:8080/mcp", "", "streamable_http"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ProviderConfig{
				Name:                  tc.name,
				Enabled:               true,
				Transport:             tc.transport,
				URL:                   tc.url,
				Command:               tc.command,
				StartupTimeoutSeconds: 10,
			}
			_, cleanup, metadata, err := NewTransportWithStore(cfg, nil)
			if err != nil {
				t.Fatalf("NewTransport: %v", err)
			}
			defer cleanup()
			if got, want := metadata.Kind, tc.wantKind; got != want {
				t.Fatalf("metadata.Kind = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildSSETransportReturnsNoOpCleanup(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "sse_cleanup",
		Enabled:               true,
		Transport:             "sse",
		URL:                   "http://localhost:8080/sse",
		StartupTimeoutSeconds: 10,
	}
	_, cleanup, _, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport sse: %v", err)
	}
	// No-op cleanup should be safe to call
	cleanup()
}

func TestBuildStreamableHTTPTransportReturnsNoOpCleanup(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "http_cleanup",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "http://localhost:8080/mcp",
		StartupTimeoutSeconds: 10,
	}
	_, cleanup, _, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport streamable_http: %v", err)
	}
	cleanup()
}

func TestNewTransport_SSEEndpointField(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "sse_endpoint_check",
		Enabled:               true,
		Transport:             "sse",
		URL:                   "http://example.com:9090/mcp/sse",
		StartupTimeoutSeconds: 10,
	}
	transport, cleanup, _, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	defer cleanup()
	sseTransport, ok := transport.(*mcp.SSEClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.SSEClientTransport, got %T", transport)
	}
	if got, want := sseTranscore.Endpoint, "http://example.com:9090/mcp/sse"; got != want {
		t.Fatalf("Endpoint = %q, want %q", got, want)
	}
}

func TestNewTransport_StreamableHTTPEndpointAndMaxRetries(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "http_endpoint_check",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "http://example.com:9090/api/mcp",
		StartupTimeoutSeconds: 10,
	}
	transport, cleanup, _, err := NewTransportWithStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	defer cleanup()
	sHttp, ok := transport.(*mcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.StreamableClientTransport, got %T", transport)
	}
	if got, want := sHttp.Endpoint, "http://example.com:9090/api/mcp"; got != want {
		t.Fatalf("Endpoint = %q, want %q", got, want)
	}
	if got, want := sHttp.MaxRetries, -1; got != want {
		t.Fatalf("MaxRetries = %d, want %d", got, want)
	}
}
