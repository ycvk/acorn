package config

import (
	"strings"
	"testing"
)

func TestValidateExecutionReadyRejectsInvalidExecutionFields(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers = []ProviderConfig{{
		Name:                "default",
		Model:               "test-model",
		BaseURL:             "https://example.invalid/v1",
		APIKey:              "test-api-key",
		MaxOutputTokens:     new(1024),
		ContextWindowTokens: new(200000),
		TimeoutSeconds:      -1,
		Enabled:             true,
	}}
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid timeout to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: timeout_seconds must be >= 0") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}

	cfg.Providers[0].TimeoutSeconds = 60
	cfg.Providers[0].MaxOutputTokens = new(0)
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid max_output_tokens to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: max_output_tokens must be positive") {
		t.Fatalf("expected max token validation error, got %v", err)
	}

	cfg.Providers[0].MaxOutputTokens = new(1024)

	cfg.Providers[0].ReasoningEffort = "invalid"
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid reasoning_effort to fail validation")
	} else if !strings.Contains(err.Error(), `provider default: reasoning_effort "invalid" is unsupported`) {
		t.Fatalf("expected reasoning_effort validation error, got %v", err)
	}

	cfg.Providers[0].ReasoningEffort = "low"
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("expected valid reasoning_effort to pass, got %v", err)
	}
}

func TestBackupKeepMustBePositiveWhenBackupsAreOn(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Backup.Keep = 0
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("backups off: %v", err)
	}
	cfg.Backup.Dir = "/srv/acorn/backups"
	if err := cfg.ValidateBase(); err == nil || !strings.Contains(err.Error(), "backup.keep") {
		t.Fatalf("backups on with keep 0: %v", err)
	}
}
