package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProviderTransport(t *testing.T) {
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

func TestLoadMCPProviderWithoutTransportRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: my_server
      enabled: true
      command: my-mcp-server
      args: []
      env: {}
      tool_names: []
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected missing MCP transport to be rejected")
	}
	if !strings.Contains(err.Error(), "mcp.providers[my_server].transport is required") {
		t.Fatalf("expected transport-required error, got %v", err)
	}
}

func TestLoadMCPProviderSSEWithURL(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: remote_sse
      enabled: true
      transport: sse
      url: http://localhost:8080/sse
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.MCP.Providers[0].Transport, "sse"; got != want {
		t.Fatalf("transport = %q, want %q", got, want)
	}
	if got, want := cfg.MCP.Providers[0].URL, "http://localhost:8080/sse"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestLoadMCPProviderSSEPathPrefixRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: prefixed_sse
      enabled: true
      transport: sse
      url: http://localhost:8080/proxy/sse
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected SSE with path-prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "prefixed_sse") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
	if !strings.Contains(err.Error(), "streamable_http") {
		t.Fatalf("expected error to mention streamable_http, got %v", err)
	}
}

func TestLoadMCPProviderStreamableHTTPWithURL(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: remote_http
      enabled: true
      transport: streamable_http
      url: http://localhost:8080/mcp
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.MCP.Providers[0].Transport, "streamable_http"; got != want {
		t.Fatalf("transport = %q, want %q", got, want)
	}
	if got, want := cfg.MCP.Providers[0].URL, "http://localhost:8080/mcp"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestLoadMCPProviderInvalidTransportRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: bad_transport
      enabled: true
      transport: websocket
      url: ws://localhost:8080
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected invalid transport to be rejected")
	}
	if !strings.Contains(err.Error(), "bad_transport") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
	if !strings.Contains(err.Error(), "must be one of stdio|sse|streamable_http") {
		t.Fatalf("expected allowed-values error, got %v", err)
	}
}

func TestLoadMCPProviderStdioWithoutCommandRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: no_command
      enabled: true
      transport: stdio
      command: ""
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected stdio provider without command to be rejected")
	}
	if !strings.Contains(err.Error(), "no_command") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
	if !strings.Contains(err.Error(), "command is required for stdio transport") {
		t.Fatalf("expected stdio command-required error, got %v", err)
	}
}

func TestLoadMCPProviderSSEWithoutURLRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test-model
    base_url: https://example.invalid/v1
    api_key: test-api-key
    temperature: 0.3
    max_completion_tokens: 777
    timeout_seconds: 45
    enabled: true
runtime:
  storage_dir: .acorn
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
  system_prompt: |
    test
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers:
    - name: no_url
      enabled: true
      transport: sse
      url: ""
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected SSE provider without URL to be rejected")
	}
	if !strings.Contains(err.Error(), "no_url") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
	if !strings.Contains(err.Error(), "url is required for sse transport") {
		t.Fatalf("expected sse url-required error, got %v", err)
	}
}
