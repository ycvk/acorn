package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThinkingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"budget", func(c *Config) { c.Wake.DailyTokens = -1 }, "wake.daily_tokens"},
		{"night", func(c *Config) { c.Thinking.NightAt = "25:00" }, "thinking.night_at"},
		{"wander", func(c *Config) { c.Thinking.WanderAt = []string{"wrong"} }, "thinking.wander_at"},
		{"duplicate", func(c *Config) { c.Thinking.WanderAt = []string{"12:00", "12:00"} }, "duplicate"},
		{"normalized duplicate", func(c *Config) { c.Thinking.WanderAt = []string{"15:00", " 15:00 "} }, "duplicate"},
		{"many", func(c *Config) { c.Thinking.WanderAt = make([]string, 7) }, "at most 6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := defaultConfig()
			tc.change(c)
			if err := c.ValidateBase(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation %v", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("thinking:\n  night_at: \"\"\n  wander_at: [\"15:00\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Thinking.NightAt != "" || len(c.Thinking.WanderAt) != 1 || c.Wake.DailyTokens != 0 {
		t.Fatalf("config %+v %+v", c.Thinking, c.Wake)
	}
}
