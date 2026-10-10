package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateBaseRejectsInvalidPresenceSettings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"timezone", func(c *Config) { c.Owner.Timezone = "Mars/Olympus" }, "owner.timezone"},
		{"daily limit", func(c *Config) { c.Wake.DailyLimit = -1 }, "wake.daily_limit"},
		{"max per hour", func(c *Config) { c.Notify.MaxPerHour = 0 }, "notify.max_per_hour"},
		{"half quiet hours", func(c *Config) { c.Notify.QuietHours.End = "" }, "needs both start and end"},
		{"bad clock", func(c *Config) { c.Notify.QuietHours.Start = "25:00" }, "notify.quiet_hours.start"},
		{"missing fcm file", func(c *Config) { c.Notify.FCM.ServiceAccountFile = "/nonexistent/sa.json" }, "notify.fcm.service_account_file"},
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

func TestValidateExecutionReadyBoundsPresenceTokens(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = "k"
	cfg.Presence.MaxTokens = 100
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err == nil || !strings.Contains(err.Error(), "presence.max_tokens") {
		t.Fatalf("small budget: %v", err)
	}
	cfg.Presence.MaxTokens = cfg.Context.CompactMarginTokens
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err == nil || !strings.Contains(err.Error(), "compact_margin_tokens") {
		t.Fatalf("budget over margin: %v", err)
	}
}

func TestLoadFCMServiceAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sa.json")
	if err := os.WriteFile(path, []byte(`{"project_id":"p","client_email":"e@p.iam","private_key":"k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFCMServiceAccount(path); err == nil || !strings.Contains(err.Error(), "token_uri") {
		t.Fatalf("missing token_uri: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"project_id":"p","client_email":"e@p.iam","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	account, err := LoadFCMServiceAccount(path)
	if err != nil || account.ProjectID != "p" {
		t.Fatalf("account = %+v err=%v", account, err)
	}
}

func TestParseClockAndOwnerLocation(t *testing.T) {
	if got, err := ParseClock("23:30"); err != nil || got != 23*time.Hour+30*time.Minute {
		t.Fatalf("ParseClock = %v err=%v", got, err)
	}
	cfg := defaultConfig()
	cfg.Owner.Timezone = "Asia/Shanghai"
	loc, err := cfg.OwnerLocation()
	if err != nil || loc.String() != "Asia/Shanghai" {
		t.Fatalf("location = %v err=%v", loc, err)
	}
	if got := cfg.PersonaPath(); got != filepath.Join(cfg.Runtime.StorageDir, "persona.md") {
		t.Fatalf("persona path = %s", got)
	}
}
