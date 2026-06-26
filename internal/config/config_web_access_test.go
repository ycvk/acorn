package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExecutionReadyDoesNotRequireWebSearchKeyOrBrowserExecutable(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = "sk-chat"
	cfg.WebAccess.Search.APIKey = ""
	cfg.Browser.ExecutablePath = ""
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("ValidateExecutionReady: %v", err)
	}
}

func TestValidateBaseRejectsInvalidWebAccessConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "empty user agent",
			mutate: func(cfg *Config) {
				cfg.WebAccess.UserAgent = ""
			},
			wantErr: "web_access.user_agent is required",
		},
		{
			name: "zero timeout",
			mutate: func(cfg *Config) {
				cfg.WebAccess.TimeoutSeconds = 0
			},
			wantErr: "web_access.timeout_seconds must be > 0",
		},
		{
			name: "zero response limit",
			mutate: func(cfg *Config) {
				cfg.WebAccess.MaxResponseBytes = 0
			},
			wantErr: "web_access.max_response_bytes must be > 0",
		},
		{
			name: "unsupported provider",
			mutate: func(cfg *Config) {
				cfg.WebAccess.Search.Provider = "google"
			},
			wantErr: "web_access.search.provider must be tavily",
		},
		{
			name: "zero search timeout",
			mutate: func(cfg *Config) {
				cfg.WebAccess.Search.TimeoutSeconds = 0
			},
			wantErr: "web_access.search.timeout_seconds must be > 0",
		},
		{
			name: "zero max results",
			mutate: func(cfg *Config) {
				cfg.WebAccess.Search.MaxResults = 0
			},
			wantErr: "web_access.search.max_results must be > 0",
		},
		{
			name: "zero browser timeout",
			mutate: func(cfg *Config) {
				cfg.Browser.DefaultTimeoutSeconds = 0
			},
			wantErr: "browser.default_timeout_seconds must be > 0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			tc.mutate(cfg)
			err := cfg.ValidateBase()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateBase error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadExpandsWebSearchKeyAndResolvesBrowserExecutable(t *testing.T) {
	t.Setenv("ACORN_TEST_TAVILY_KEY", "tvly-test")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	body := `providers:
  - name: default
    model: test
    base_url: https://example.invalid/v1
    api_key: test
    temperature: 0.3
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
web_access:
  search:
    api_key: ${ACORN_TEST_TAVILY_KEY}
browser:
  executable_path: bin/chromium
agent:
  name: coordinator
  description: test
  max_iterations: 4
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
  providers: []
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.WebAccess.Search.APIKey; got != "tvly-test" {
		t.Fatalf("web_access.search.api_key = %q, want expanded value", got)
	}
	if got, want := cfg.Browser.ExecutablePath, filepath.Join(dir, "bin/chromium"); got != want {
		t.Fatalf("browser.executable_path = %q, want %q", got, want)
	}
}
