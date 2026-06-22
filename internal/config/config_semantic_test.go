package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExpandsMemorySemanticEmbeddingAPIKeyEnvironment(t *testing.T) {
	t.Setenv("ACORN_TEST_CHAT_KEY", "sk-chat")
	t.Setenv("ACORN_TEST_EMBEDDING_KEY", "sk-embedding")

	cfg, err := Load(writeConfig(t, `providers:
  - name: default
    model: test
    base_url: https://example.invalid/v1
    api_key: ${ACORN_TEST_CHAT_KEY}
    temperature: 0.3
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers: []
memory:
  search:
    memory_context_token_budget: 2000
  semantic:
    embedding:
      provider: openai_compatible
      model: text-embedding-3-small
      base_url: https://api.openai.com/v1
      api_key: ${ACORN_TEST_EMBEDDING_KEY}
      dimensions: 1536
      timeout_seconds: 30
      batch_size: 64
`))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.Memory.Semantic.Embedding.APIKey, "sk-embedding"; got != want {
		t.Fatalf("embedding api_key = %q, want %q", got, want)
	}
}

func TestValidateMemorySemanticReadyDoesNotRequireChatProviderAPIKey(t *testing.T) {
	cfg := validSemanticConfig()
	cfg.Providers[0].APIKey = ""

	if err := cfg.ValidateMemorySemanticReady(); err != nil {
		t.Fatalf("ValidateMemorySemanticReady: %v", err)
	}
}

func TestValidateExecutionReadyAllowsUnconfiguredMemorySemantic(t *testing.T) {
	cfg := validSemanticConfig()
	cfg.Memory.Semantic.Embedding.Model = ""
	cfg.Memory.Semantic.Embedding.BaseURL = ""
	cfg.Memory.Semantic.Embedding.APIKey = ""

	if cfg.MemorySemanticConfigured() {
		t.Fatal("clearing embedding model+base_url should report semantic retrieval as unconfigured")
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("unconfigured semantic retrieval must not block execution readiness: %v", err)
	}
}

func TestValidateExecutionReadyMemorySemanticRequiresIndependentAPIKey(t *testing.T) {
	cfg := validSemanticConfig()
	cfg.Memory.Semantic.Embedding.APIKey = ""

	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected missing semantic embedding api_key to fail")
	} else if !strings.Contains(err.Error(), "memory.semantic.embedding.api_key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExecutionReadyMemorySemanticRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "embedding model",
			mutate: func(cfg *Config) {
				cfg.Memory.Semantic.Embedding.Model = ""
			},
			wantErr: "memory.semantic.embedding.model is required",
		},
		{
			name: "embedding base_url",
			mutate: func(cfg *Config) {
				cfg.Memory.Semantic.Embedding.BaseURL = ""
			},
			wantErr: "memory.semantic.embedding.base_url is required",
		},
		{
			name: "embedding dimensions",
			mutate: func(cfg *Config) {
				cfg.Memory.Semantic.Embedding.Dimensions = 0
			},
			wantErr: "memory.semantic.embedding.dimensions must be > 0",
		},
		{
			name: "embedding timeout",
			mutate: func(cfg *Config) {
				cfg.Memory.Semantic.Embedding.TimeoutSeconds = 0
			},
			wantErr: "memory.semantic.embedding.timeout_seconds must be > 0",
		},
		{
			name: "embedding batch size",
			mutate: func(cfg *Config) {
				cfg.Memory.Semantic.Embedding.BatchSize = 0
			},
			wantErr: "memory.semantic.embedding.batch_size must be > 0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSemanticConfig()
			tc.mutate(cfg)
			if err := cfg.ValidateExecutionReady(); err == nil {
				t.Fatalf("expected validation error containing %q", tc.wantErr)
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateExecutionReady error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateMemorySemanticEnums(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "provider",
			mutate: func(cfg *Config) {
				cfg.Memory.Semantic.Embedding.Provider = "chat_provider"
			},
			wantErr: "memory.semantic.embedding.provider must be openai_compatible",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSemanticConfig()
			tc.mutate(cfg)
			if err := cfg.ValidateExecutionReady(); err == nil {
				t.Fatalf("expected validation error containing %q", tc.wantErr)
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateExecutionReady error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadRejectsRemovedMemorySemanticEnabledField(t *testing.T) {
	_, err := Load(writeConfig(t, `providers:
  - name: default
    model: test
    base_url: https://example.invalid/v1
    api_key: test
    temperature: 0.3
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers: []
memory:
  search:
    memory_context_token_budget: 2000
  semantic:
    enabled: false
`))
	if err == nil {
		t.Fatal("expected removed semantic enabled field to fail strict load")
	}
	if !strings.Contains(err.Error(), "field enabled not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsRemovedLayeredMemoryBudgetFields(t *testing.T) {
	for _, field := range []string{"index_token_budget", "initial_token_budget", "on_demand_reserve"} {
		t.Run(field, func(t *testing.T) {
			_, err := Load(writeConfig(t, `providers:
  - name: default
    model: test
    base_url: https://example.invalid/v1
    api_key: test
    temperature: 0.3
    max_completion_tokens: 100
    timeout_seconds: 30
    enabled: true
web:
  listen_addr: 127.0.0.1:8080
agent:
  name: coordinator
  description: test
  max_iterations: 4
tools:
  workspace:
    root_dir: .
  mutation:
    disabled: true
    root_dir: .
  run_command:
    disabled: true
    default_timeout: 30
    work_dir: .
mcp:
  providers: []
memory:
  search:
    memory_context_token_budget: 2000
    `+field+`: 100
  semantic:
    embedding:
      provider: openai_compatible
      model: text-embedding-3-small
      base_url: https://api.openai.com/v1
      api_key: test
      dimensions: 1536
      timeout_seconds: 30
      batch_size: 64
`))
			if err == nil {
				t.Fatalf("expected removed %s field to fail strict load", field)
			}
			if !strings.Contains(err.Error(), "field "+field+" not found") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadExamplesConfigureMemorySemantic(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")

	for _, path := range []string{"../../configs/acorn.example.yaml", "../../configs/acorn.selfhosted.example.yaml"} {
		t.Run(path, func(t *testing.T) {
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("load example: %v", err)
			}
			if got, want := cfg.Memory.Semantic.Embedding.APIKey, "sk-test"; got != want {
				t.Fatalf("embedding api_key = %q, want expanded env value", got)
			}
		})
	}
}

func validSemanticConfig() *Config {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = "sk-chat"
	// Semantic is OFF by default now (no model/base_url defaults); set them to
	// represent an explicitly semantic-enabled config for these checks.
	cfg.Memory.Semantic.Embedding.Model = "text-embedding-3-small"
	cfg.Memory.Semantic.Embedding.BaseURL = "https://api.openai.com/v1"
	cfg.Memory.Semantic.Embedding.APIKey = "sk-embedding"
	cfg.Agent.MaxIterations = 4
	cfg.Tools.RunCommand.Disabled = true
	return cfg
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "acorn.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
