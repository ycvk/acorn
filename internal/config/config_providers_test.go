package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateExecutionReady_MultipleEnabledProvidersInvalid(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{
				Name:                "primary",
				Model:               "gpt-4o",
				BaseURL:             "https://api.openai.com/v1",
				APIKey:              "sk-primary",
				TimeoutSeconds:      30,
				MaxCompletionTokens: 2048,
				Enabled:             true,
			},
			{
				Name:                "fallback",
				Model:               "gpt-4o-mini",
				BaseURL:             "https://api.openai.com/v1",
				APIKey:              "sk-fallback",
				TimeoutSeconds:      30,
				MaxCompletionTokens: 2048,
				Enabled:             true,
			},
		},
		Runtime: RuntimeConfig{
			StorageDir: ".acorn",
		},
		Context: ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Web: WebConfig{ListenAddr: "127.0.0.1:8080"},
		Agent: AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 4,
		},
		Tools: ToolsConfig{
			Workspace: WorkspaceToolConfig{RootDir: "."},
			Mutation:  MutationToolConfig{RootDir: "."},
			RunCommand: RunCommandToolConfig{
				WorkDir: ".",
			},
		},
		Memory: defaultConfig().Memory,
	}
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected multiple enabled providers to fail validation")
	} else if !strings.Contains(err.Error(), "exactly one provider must be enabled, got 2") {
		t.Fatalf("expected exactly-one-provider validation error, got %v", err)
	}
}

func TestValidateExecutionReady_NoEnabledProviders(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].Enabled = false
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected zero enabled providers to fail validation")
	} else if !strings.Contains(err.Error(), "at least one provider must be enabled") {
		t.Fatalf("expected no-enabled-providers error, got %v", err)
	}
}

func TestValidateExecutionReady_MissingProviderName(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].Name = ""
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected missing provider name to fail validation")
	} else if !strings.Contains(err.Error(), "provider name is required") {
		t.Fatalf("expected provider name validation error, got %v", err)
	}
}

func TestValidateExecutionReady_MissingProviderModel(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].Model = ""
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected missing provider model to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: model is required") {
		t.Fatalf("expected provider model validation error, got %v", err)
	}
}

func TestValidateExecutionReady_MissingProviderBaseURL(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].BaseURL = ""
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected missing provider base_url to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: base_url is required") {
		t.Fatalf("expected provider base_url validation error, got %v", err)
	}
}

func TestValidateExecutionReady_MissingProviderAPIKey(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = ""
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected missing provider api_key to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: api_key is required") {
		t.Fatalf("expected provider api_key validation error, got %v", err)
	}
}

func TestValidateExecutionReady_ZeroTimeout(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = "test-key"
	cfg.Providers[0].TimeoutSeconds = 0
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected zero timeout to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: timeout_seconds must be > 0") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}
}

func TestValidateExecutionReady_ZeroMaxTokens(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].APIKey = "test-key"
	cfg.Providers[0].MaxCompletionTokens = 0
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected zero max_completion_tokens to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: max_completion_tokens must be > 0") {
		t.Fatalf("expected max_completion_tokens validation error, got %v", err)
	}
}

func TestValidateExecutionReady_DuplicateNames(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{
				Name:                "same",
				Model:               "gpt-4o",
				BaseURL:             "https://api.openai.com/v1",
				APIKey:              "sk-1",
				TimeoutSeconds:      30,
				MaxCompletionTokens: 2048,
				Enabled:             true,
			},
			{
				Name:                "same",
				Model:               "gpt-4o-mini",
				BaseURL:             "https://api.openai.com/v1",
				APIKey:              "sk-2",
				TimeoutSeconds:      30,
				MaxCompletionTokens: 2048,
				Enabled:             true,
			},
		},
		Runtime: RuntimeConfig{
			StorageDir: ".acorn",
		},
		Context: ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Web: WebConfig{ListenAddr: "127.0.0.1:8080"},
		Agent: AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 4,
		},
		Tools: ToolsConfig{
			Workspace: WorkspaceToolConfig{RootDir: "."},
			Mutation:  MutationToolConfig{RootDir: "."},
			RunCommand: RunCommandToolConfig{
				WorkDir: ".",
			},
		},
		Memory: defaultConfig().Memory,
	}
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected duplicate provider names to fail validation")
	} else if !strings.Contains(err.Error(), `duplicate enabled provider name "same"`) {
		t.Fatalf("expected duplicate name validation error, got %v", err)
	}
}

func TestValidateExecutionReady_DisabledProviderNotValidated(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{
				Name:                "primary",
				Model:               "gpt-4o",
				BaseURL:             "https://api.openai.com/v1",
				APIKey:              "sk-primary",
				TimeoutSeconds:      30,
				MaxCompletionTokens: 2048,
				Enabled:             true,
			},
			{
				Name:                "",
				Model:               "",
				BaseURL:             "",
				APIKey:              "",
				TimeoutSeconds:      0,
				MaxCompletionTokens: 0,
				Enabled:             false,
			},
		},
		Runtime: RuntimeConfig{
			StorageDir: ".acorn",
		},
		Context: ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Web: WebConfig{ListenAddr: "127.0.0.1:8080"},
		Agent: AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 4,
		},
		Tools: ToolsConfig{
			Workspace: WorkspaceToolConfig{RootDir: "."},
			Mutation:  MutationToolConfig{RootDir: "."},
			RunCommand: RunCommandToolConfig{
				WorkDir: ".",
			},
		},
		Memory: defaultConfig().Memory,
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("expected disabled provider to be skipped during validation, got %v", err)
	}
}

func TestConfig_APIKey(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Enabled: false, APIKey: "disabled-key"},
			{Enabled: true, APIKey: "first-enabled-key"},
			{Enabled: true, APIKey: "second-enabled-key"},
		},
	}
	if got, want := cfg.APIKey(), "first-enabled-key"; got != want {
		t.Fatalf("APIKey = %q, want %q", got, want)
	}
}

func TestConfig_EnabledProviders(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "a", Enabled: false},
			{Name: "b", Enabled: true},
			{Name: "c", Enabled: false},
			{Name: "d", Enabled: true},
		},
	}
	enabled := cfg.EnabledProviders()
	if got, want := len(enabled), 2; got != want {
		t.Fatalf("len(EnabledProviders) = %d, want %d", got, want)
	}
	if got, want := enabled[0].Name, "b"; got != want {
		t.Fatalf("enabled[0].Name = %q, want %q", got, want)
	}
	if got, want := enabled[1].Name, "d"; got != want {
		t.Fatalf("enabled[1].Name = %q, want %q", got, want)
	}
}

func TestYAML_LoadProvider(t *testing.T) {
	configBody := `providers:
  - name: primary
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: sk-test
    timeout_seconds: 30
    enabled: true
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(configBody), &cfg); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if got, want := len(cfg.Providers), 1; got != want {
		t.Fatalf("len(providers) = %d, want %d", got, want)
	}
	if got, want := cfg.Providers[0].Name, "primary"; got != want {
		t.Fatalf("providers[0].Name = %q, want %q", got, want)
	}
	if got, want := cfg.Providers[0].Model, "gpt-4o"; got != want {
		t.Fatalf("providers[0].Model = %q, want %q", got, want)
	}
}
