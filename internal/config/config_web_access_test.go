package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExecutionReadyDoesNotRequireWebSearchKey(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = "sk-chat"
	cfg.WebAccess.Search.APIKey = ""
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("ValidateExecutionReady: %v", err)
	}
}

func TestValidateBaseRejectsAnotherSearchProvider(t *testing.T) {
	cfg := defaultConfig()
	cfg.WebAccess.Search.Provider = "google"
	if err := cfg.ValidateBase(); err == nil || !strings.Contains(err.Error(), "web_access.search.provider must be exa") {
		t.Fatalf("ValidateBase error = %v", err)
	}
}

func TestLoadExpandsWebSearchKey(t *testing.T) {
	t.Setenv("ACORN_TEST_EXA_KEY", "exa-test")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	body := `providers:
  - name: default
    model: test
    base_url: https://example.invalid/v1
    api_key: test
    temperature: 0.3
    max_output_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
web_access:
  search:
    api_key: ${ACORN_TEST_EXA_KEY}
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
	if got := cfg.WebAccess.Search.APIKey; got != "exa-test" {
		t.Fatalf("web_access.search.api_key = %q, want expanded value", got)
	}
}
