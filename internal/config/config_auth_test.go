package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMCPAuthOauthWithStdioRejected(t *testing.T) {
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
    - name: oauth_stdio
      enabled: true
      transport: stdio
      command: my-server
      startup_timeout_seconds: 30
      tool_safety: read_only
      auth:
        type: oauth
        client_id: my-client
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected oauth+stdio to be rejected")
	}
	if !strings.Contains(err.Error(), "oauth_stdio") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
	if !strings.Contains(err.Error(), "oauth") {
		t.Fatalf("expected oauth-related error, got %v", err)
	}
}

func TestLoadMCPAuthOauthWithEmptyTransportRejected(t *testing.T) {
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
    - name: oauth_notransport
      enabled: true
      command: my-server
      startup_timeout_seconds: 30
      tool_safety: read_only
      auth:
        type: oauth
        client_id: my-client
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected oauth with empty transport to be rejected")
	}
	if !strings.Contains(err.Error(), "oauth_notransport") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
}

func TestLoadMCPAuthOauthWithSSEAccepted(t *testing.T) {
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
    - name: oauth_sse
      enabled: true
      transport: sse
      url: http://localhost:8080/sse
      startup_timeout_seconds: 30
      tool_safety: read_only
      auth:
        type: oauth
        client_id: my-client
        scopes:
          - read
          - write
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("expected oauth+sse to be accepted, got error: %v", err)
	}
	if got, want := cfg.MCP.Providers[0].Auth.Type, "oauth"; got != want {
		t.Fatalf("auth.type = %q, want %q", got, want)
	}
	if got, want := cfg.MCP.Providers[0].Auth.ClientID, "my-client"; got != want {
		t.Fatalf("auth.client_id = %q, want %q", got, want)
	}
	if got, want := len(cfg.MCP.Providers[0].Auth.Scopes), 2; got != want {
		t.Fatalf("len(auth.scopes) = %d, want %d", got, want)
	}
}

func TestLoadMCPAuthInvalidTypeRejected(t *testing.T) {
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
    - name: bad_auth
      enabled: true
      transport: sse
      url: http://localhost:8080/sse
      startup_timeout_seconds: 30
      tool_safety: read_only
      auth:
        type: kerberos
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected invalid auth.type to be rejected")
	}
	if !strings.Contains(err.Error(), "bad_auth") {
		t.Fatalf("expected provider-named error, got %v", err)
	}
	if !strings.Contains(err.Error(), "auth.type must be one of none, oauth, api_key") {
		t.Fatalf("expected auth.type validation error, got %v", err)
	}
}

func TestLoadMCPAuthNoneDefaultOnEmpty(t *testing.T) {
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
    - name: no_auth
      enabled: true
      transport: stdio
      command: my-server
      startup_timeout_seconds: 30
      tool_safety: read_only
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("expected config without auth to be accepted, got error: %v", err)
	}
	// Auth.Type defaults to empty string, treated as "none" at runtime
	if got, want := cfg.MCP.Providers[0].Auth.Type, ""; got != want {
		t.Fatalf("auth.type = %q, want %q (empty = defaults to none)", got, want)
	}
}
