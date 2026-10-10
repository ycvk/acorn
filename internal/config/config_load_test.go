package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/.acorn", filepath.Join(home, ".acorn")},
		{"~/", home},
		{"~", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~user/something", "~user/something"},
		{"", ""},
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveDirExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	got := resolveDir("/any/config/dir", "~/.acorn")
	want := filepath.Join(home, ".acorn")
	if got != want {
		t.Errorf("resolveDir(_, ~/.acorn) = %q, want %q", got, want)
	}
}

func TestLoadExpandsHomeConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".acorn")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configBody := `providers:
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
agent:
  name: coordinator
  description: test
  max_iterations: 4
mcp:
  providers: []
`
	if err := os.WriteFile(filepath.Join(configDir, "acorn.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load("~/.acorn/acorn.yaml")
	if err != nil {
		t.Fatalf("load home config path: %v", err)
	}
	if cfg.ConfigPath != filepath.Join(configDir, "acorn.yaml") {
		t.Fatalf("config path = %q, want %q", cfg.ConfigPath, filepath.Join(configDir, "acorn.yaml"))
	}
}

func TestLoadExpandsProviderAPIKeyEnvironment(t *testing.T) {
	t.Setenv("ACORN_TEST_API_KEY", "sk-from-env")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
  - name: default
    model: test
    base_url: https://example.invalid/v1
    api_key: ${ACORN_TEST_API_KEY}
    temperature: 0.3
    max_output_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
mcp:
  providers: []
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Providers[0].APIKey; got != "sk-from-env" {
		t.Fatalf("provider api_key = %q, want env-expanded value", got)
	}
}

func TestLoadReportsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acorn.yaml")
	if err := os.WriteFile(path, []byte("totally_unknown_field: 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown config field")
	}
	if !strings.Contains(err.Error(), "totally_unknown_field") {
		t.Fatalf("error should name the unknown field, got: %v", err)
	}
}

func TestLoadDefaultsToHomeAcorn(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	configBody := `providers:
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
agent:
  name: coordinator
  description: test
  max_iterations: 4
mcp:
  providers: []
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	want := filepath.Join(home, ".acorn")
	if cfg.Runtime.StorageDir != want {
		t.Errorf("storage_dir = %q, want %q", cfg.Runtime.StorageDir, want)
	}
}

func TestLoadRejectsEmptyPath(t *testing.T) {
	if _, err := Load("  "); err == nil {
		t.Fatal("Load with empty path must fail instead of falling back to a repo-relative example config")
	}
}
