package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalRulesRejectBadPatternsAndAskOperator(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"bad block", func(c *Config) { c.Approval.Block = []string{"["} }, "approval.block[0]"},
		{"bad allow", func(c *Config) { c.Approval.Allow = []string{"ok", "["} }, "approval.allow[1]"},
		{"block ask_operator", func(c *Config) { c.Approval.Block = []string{"ask_*"} }, "must not match ask_operator"},
		{"review model", func(c *Config) { c.Review.Model = " " }, "review.model"},
		{"review timeout", func(c *Config) { c.Review.TimeoutSeconds = 0 }, "review.timeout_seconds"},
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
	cfg.Approval.Allow = []string{"*"}
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("allow may match ask_operator: %v", err)
	}
}

func TestReviewTurnsOnWithAnExpandedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	if err := os.WriteFile(path, []byte("approval:\n  block: [\"mcp__bank__*\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReviewEnabled() || cfg.Review.Model != "jev-preview" || cfg.Review.TimeoutSeconds != 20 || cfg.Approval.Block[0] != "mcp__bank__*" {
		t.Fatalf("review = %+v approval = %+v", cfg.Review, cfg.Approval)
	}
	t.Setenv("TYPESAFE_API_KEY", "apikey_test")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReviewEnabled() || cfg.Review.APIKey != "apikey_test" {
		t.Fatalf("review = %+v", cfg.Review)
	}
}
