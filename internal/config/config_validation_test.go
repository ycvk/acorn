package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExecutionReadyContextConfig(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{{
			Name:                "default",
			Model:               "gpt-4.1-mini",
			BaseURL:             "https://example.invalid/v1",
			APIKey:              "chat-key",
			MaxCompletionTokens: 1024,
			TimeoutSeconds:      30,
			Enabled:             true,
		}},
		Context: ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Memory: defaultConfig().Memory,
		Runtime: RuntimeConfig{
			StorageDir: filepath.Join(t.TempDir(), ".acorn"),
		},
		Web:       WebConfig{ListenAddr: "127.0.0.1:8080"},
		WebAccess: defaultConfig().WebAccess,
		Browser:   defaultConfig().Browser,
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
	}

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "context window",
			mutate: func(cfg *Config) {
				cfg.Context.WindowTokens = 0
			},
			wantErr: "context.window_tokens must be > 0",
		},
		{
			name: "compact margin tokens",
			mutate: func(cfg *Config) {
				cfg.Context.CompactMarginTokens = 1
			},
			wantErr: "context.compact_margin_tokens must be > 1",
		},
		{
			name: "preserve recent turns",
			mutate: func(cfg *Config) {
				cfg.Context.PreserveRecentTurns = 0
			},
			wantErr: "context.preserve_recent_turns must be >= 1",
		},
		{
			name: "summary max tokens",
			mutate: func(cfg *Config) {
				cfg.Context.SummaryMaxTokens = 0
			},
			wantErr: "context.summary_max_tokens must be > 0",
		},
		{
			name: "effective window too small",
			mutate: func(cfg *Config) {
				cfg.Context.WindowTokens = cfg.Context.CompactMarginTokens + defaultContextWarningGapTokens + defaultContextStaticOverheadTokens
			},
			wantErr: "context effective window must be greater than derived warning threshold buffer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *cfg
			tc.mutate(&candidate)
			if err := candidate.ValidateExecutionReady(); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateExecutionReady error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateExecutionReadyRejectsInvalidExecutionFields(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{{
			Name:                "default",
			Model:               "test-model",
			BaseURL:             "https://example.invalid/v1",
			APIKey:              "test-api-key",
			MaxCompletionTokens: 1024,
			TimeoutSeconds:      60,
			Enabled:             true,
		}},
		Runtime: RuntimeConfig{
			StorageDir: ".acorn",
		},
		Context: ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Web:       WebConfig{ListenAddr: "127.0.0.1:8080"},
		WebAccess: defaultConfig().WebAccess,
		Browser:   defaultConfig().Browser,
		Agent: AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 0,
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
		t.Fatal("expected invalid execution fields to fail validation")
	} else if !strings.Contains(err.Error(), "agent.max_iterations must be > 0") {
		t.Fatalf("expected max_iterations validation error, got %v", err)
	}

	cfg.Agent.MaxIterations = 4
	cfg.Providers[0].TimeoutSeconds = 0
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid timeout to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: timeout_seconds must be > 0") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}

	cfg.Providers[0].TimeoutSeconds = 60
	cfg.Providers[0].MaxCompletionTokens = 0
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid max_completion_tokens to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: max_completion_tokens must be > 0") {
		t.Fatalf("expected max token validation error, got %v", err)
	}

	cfg.Providers[0].MaxCompletionTokens = 1024

	cfg.Providers[0].ReasoningEffort = "invalid"
	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid reasoning_effort to fail validation")
	} else if !strings.Contains(err.Error(), "provider default: reasoning_effort must be low, medium, or high") {
		t.Fatalf("expected reasoning_effort validation error, got %v", err)
	}

	cfg.Providers[0].ReasoningEffort = "low"
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("expected valid reasoning_effort to pass, got %v", err)
	}
}

func TestWorkspaceRootDirRejectsMismatchedLocalToolRoots(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	cfg := defaultConfig()
	cfg.Tools.Workspace.RootDir = root
	cfg.Tools.Mutation.RootDir = other
	cfg.Tools.RunCommand.WorkDir = root

	_, err := cfg.Workspace()
	if err == nil {
		t.Fatal("expected mismatched local tool roots to fail")
	}
	if !strings.Contains(err.Error(), "workspace root mismatch") {
		t.Fatalf("Workspace error = %v, want workspace root mismatch", err)
	}
}
