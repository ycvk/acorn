package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewSettingsAreChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
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
}

func TestReviewTurnsOnWithAnExpandedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	if err := os.WriteFile(path, []byte("review:\n  timeout_seconds: 20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReviewEnabled() || cfg.Review.Model != "jev-preview" || cfg.Review.TimeoutSeconds != 20 {
		t.Fatalf("review = %+v", cfg.Review)
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
