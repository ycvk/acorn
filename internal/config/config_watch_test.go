package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBaseRejectsInvalidWatchSettings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"rsshub url", func(c *Config) { c.Watch.RSSHubBaseURL = "rsshub.local" }, "watch.rsshub_base_url"},
		{"checks per tick", func(c *Config) { c.Watch.MaxChecksPerTick = 0 }, "watch.max_checks_per_tick"},
		{"briefing clock", func(c *Config) { c.Briefing.At = "8am" }, "briefing.at"},
		{"news budget", func(c *Config) { c.News.DailyTokens = -1 }, "news.daily_tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			tc.mutate(cfg)
			if err := cfg.ValidateBase(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateBase() = %v, want %q", err, tc.want)
			}
		})
	}
	cfg := defaultConfig()
	cfg.Briefing.At = ""
	cfg.Watch.RSSHubBaseURL = "http://127.0.0.1:1200"
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("briefing off and a local RSSHub are valid: %v", err)
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

func TestNewsBudgetDefaultsAndLoads(t *testing.T) {
	if got := defaultConfig().News.DailyTokens; got != 100000 {
		t.Fatalf("default news.daily_tokens = %d", got)
	}
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	if err := os.WriteFile(path, []byte("news:\n  daily_tokens: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.News.DailyTokens != 0 {
		t.Fatalf("news.daily_tokens = %d, %v", cfg.News.DailyTokens, err)
	}
}
