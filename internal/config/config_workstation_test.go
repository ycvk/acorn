package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBaseRejectsInvalidWorkstationSettings(t *testing.T) {
	token := strings.Repeat("t", 32)
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"timeout", func(c *Config) { c.Workstation.CommandTimeoutSeconds = 0 }, "workstation.command_timeout_seconds"},
		{"output", func(c *Config) { c.Workstation.MaxOutputBytes = 10 }, "workstation.max_output_bytes"},
		{"url", func(c *Config) { c.Workstation.URL = "127.0.0.1:7810"; c.Workstation.Token = token }, "workstation.url"},
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

// Backups and init load the config without the env file, so a missing token
// only blocks execution.
func TestWorkstationTokenIsRequiredOnlyForExecution(t *testing.T) {
	t.Setenv("ACORN_WORKSTATION_TOKEN", "")
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	if err := os.WriteFile(path, []byte("workstation:\n  url: http://127.0.0.1:7810\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load without the token: %v", err)
	}
	cfg.Providers[0].APIKey = "k"
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err == nil || !strings.Contains(err.Error(), "ACORN_WORKSTATION_TOKEN") {
		t.Fatalf("ValidateExecutionReady() = %v", err)
	}
}

func TestLoadExpandsWorkstationToken(t *testing.T) {
	token := strings.Repeat("w", 32)
	t.Setenv("ACORN_WORKSTATION_TOKEN", token)
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	body := "workstation:\n  url: http://127.0.0.1:7810\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workstation.Token != token || cfg.Workstation.CommandTimeoutSeconds != 300 || cfg.Workstation.MaxOutputBytes != 32768 {
		t.Fatalf("workstation = %+v", cfg.Workstation)
	}
}
