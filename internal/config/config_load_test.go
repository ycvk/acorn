package config

import (
	"os"
	"path/filepath"
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
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
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
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
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

func TestLoadSelfHostedExample(t *testing.T) {
	t.Setenv("HOME", "/root")
	t.Setenv("OPENAI_API_KEY", "sk-test")

	cfg, err := Load("../../configs/acorn.selfhosted.example.yaml")
	if err != nil {
		t.Fatalf("load self-hosted example: %v", err)
	}
	if got := cfg.Providers[0].APIKey; got != "sk-test" {
		t.Fatalf("provider api_key = %q, want env-expanded test key", got)
	}
	if got, want := cfg.Runtime.StorageDir, "/root/.acorn"; got != want {
		t.Fatalf("runtime.storage_dir = %q, want %q", got, want)
	}
	if got := cfg.Web.ListenAddr; got != "127.0.0.1:8080" {
		t.Fatalf("web.listen_addr = %q, want 127.0.0.1:8080", got)
	}
	if got := cfg.Tools.Workspace.RootDir; got != "/srv/acorn/workspace" {
		t.Fatalf("tools.workspace.root_dir = %q, want /srv/acorn/workspace", got)
	}
	if got := cfg.Tools.RunCommand.WorkDir; got != "/srv/acorn/workspace" {
		t.Fatalf("tools.run_command.work_dir = %q, want /srv/acorn/workspace", got)
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
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
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
