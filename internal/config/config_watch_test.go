package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBaseChecksTheRSSHubAddress(t *testing.T) {
	cfg := defaultConfig()
	cfg.Watch.RSSHubBaseURL = "rsshub.local"
	if err := cfg.ValidateBase(); err == nil || !strings.Contains(err.Error(), "watch.rsshub_base_url") {
		t.Fatalf("ValidateBase() = %v", err)
	}
	cfg.Watch.RSSHubBaseURL = "http://127.0.0.1:1200"
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("a local RSSHub is valid: %v", err)
	}
}

func TestGitHubTokenExpandsEnv(t *testing.T) {
	t.Setenv("ACORN_TEST_GITHUB_TOKEN", "ghp_from_env")
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	if err := os.WriteFile(path, []byte("watch:\n  github_token: ${ACORN_TEST_GITHUB_TOKEN}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Watch.GitHubToken != "ghp_from_env" {
		t.Fatalf("github token = %q", cfg.Watch.GitHubToken)
	}
}
