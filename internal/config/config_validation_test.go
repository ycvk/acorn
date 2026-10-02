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
			MaskAfterTurns:      2,
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
			name: "compact margin exceeds window",
			mutate: func(cfg *Config) {
				cfg.Context.CompactMarginTokens = cfg.Context.WindowTokens
			},
			wantErr: "context.compact_margin_tokens must be < context.window_tokens",
		},
		{
			name: "mask after turns",
			mutate: func(cfg *Config) {
				cfg.Context.MaskAfterTurns = -1
			},
			wantErr: "context.mask_after_turns must be >= 0",
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
			MaskAfterTurns:      2,
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
		},
		Memory: defaultConfig().Memory,
	}

	if err := cfg.ValidateExecutionReady(); err == nil {
		t.Fatal("expected invalid execution fields to fail validation")
	} else if !strings.Contains(err.Error(), "runtime.max_iterations must be > 0") {
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

func TestWorkspaceRootIsCleanedRootDir(t *testing.T) {
	cfg := defaultConfig()
	cfg.Tools.Workspace.RootDir = "/srv/acorn/workspace/"
	if got, want := cfg.WorkspaceRoot(), "/srv/acorn/workspace"; got != want {
		t.Fatalf("WorkspaceRoot() = %q, want %q", got, want)
	}
	cfg.Tools.Workspace.RootDir = "  "
	if got := cfg.WorkspaceRoot(); got != "" {
		t.Fatalf("WorkspaceRoot() for blank root = %q, want empty", got)
	}
}

func TestValidateBaseRejectsNegativeReviewInterval(t *testing.T) {
	cfg := defaultConfig()
	cfg.Memory.Review.ReviewInterval = -1
	if err := cfg.ValidateBase(); err == nil {
		t.Fatal("ValidateBase must reject negative review_interval")
	}
}

func TestValidateBaseRejectsMalformedApprovalPattern(t *testing.T) {
	cfg := defaultConfig()
	cfg.Approval.Require = []string{"["}
	err := cfg.ValidateBase()
	if err == nil || !strings.Contains(err.Error(), "approval.require[0]") {
		t.Fatalf("ValidateBase() error = %v, want approval.require[0] error", err)
	}
}

func TestValidateBaseRejectsApprovalPatternMatchingAskOperator(t *testing.T) {
	for _, pattern := range []string{"ask_operator", "*", "ask_*"} {
		cfg := defaultConfig()
		cfg.Approval.Require = []string{"browser", pattern}
		err := cfg.ValidateBase()
		if err == nil || !strings.Contains(err.Error(), "approval.require[1]") || !strings.Contains(err.Error(), "ask_operator") {
			t.Fatalf("pattern %q: ValidateBase() error = %v, want ask_operator rejection", pattern, err)
		}
	}
}

func TestDefaultApprovalRequiresBrowserAndMCP(t *testing.T) {
	got := defaultConfig().Approval.Require
	if len(got) != 2 || got[0] != "browser" || got[1] != "mcp__*" {
		t.Fatalf("default approval.require = %v", got)
	}
}
