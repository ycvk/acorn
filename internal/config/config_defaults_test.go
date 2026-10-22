package config

import (
	"testing"
)

func TestDefaultConfigValues(t *testing.T) {
	cfg := defaultConfig()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"web_access.allow_private_networks", cfg.WebAccess.AllowPrivateNetworks, false},
		{"web_access.search.provider", cfg.WebAccess.Search.Provider, "exa"},
		{"runtime.storage_dir", cfg.Runtime.StorageDir, "~/.acorn"},
		{"backup.keep", cfg.Backup.Keep, 14},
		{"files.max_upload_mib", cfg.Files.MaxUploadMiB, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
