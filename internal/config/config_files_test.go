package config

import (
	"strings"
	"testing"
)

func TestValidateBaseRejectsFileCapsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"upload zero", func(c *Config) { c.Files.MaxUploadMiB = 0 }, "files.max_upload_mib"},
		{"upload huge", func(c *Config) { c.Files.MaxUploadMiB = 2048 }, "files.max_upload_mib"},
		{"send zero", func(c *Config) { c.Files.MaxSendMiB = 0 }, "files.max_send_mib"},
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
	if cfg.Files.MaxUploadBytes() != 50<<20 || cfg.Files.MaxSendBytes() != 100<<20 {
		t.Fatalf("default caps = %d, %d bytes", cfg.Files.MaxUploadBytes(), cfg.Files.MaxSendBytes())
	}
}
