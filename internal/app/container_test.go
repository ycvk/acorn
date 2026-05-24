package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestNewContainerFailsWhenMemorySemanticConfigMissing(t *testing.T) {
	cfg := testContainerConfig(t)
	cfg.Memory.Semantic = config.MemorySemanticConfig{}

	_, err := NewContainer(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected missing memory semantic config to fail")
	}
	if !strings.Contains(err.Error(), "memory.semantic.bleve.index_name is required") {
		t.Fatalf("NewContainer error = %v, want missing semantic config error", err)
	}
}

func testContainerConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Runtime: config.RuntimeConfig{
			StorageDir: filepath.Join(t.TempDir(), ".acorn"),
		},
		Web: config.WebConfig{ListenAddr: "127.0.0.1:8080"},
		WebAccess: config.WebAccessConfig{
			UserAgent:        "Acorn test",
			TimeoutSeconds:   20,
			MaxResponseBytes: 10 * 1024 * 1024,
			Search: config.WebSearchConfig{
				Provider:       "tavily",
				TimeoutSeconds: 10,
				MaxResults:     10,
			},
		},
		Browser: config.BrowserConfig{
			Headless:              true,
			DefaultTimeoutSeconds: 20,
		},
		Agent: config.AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 4,
		},
		Providers: []config.ProviderConfig{{
			Enabled:             true,
			Name:                "openai",
			BaseURL:             "https://example.com/v1",
			APIKey:              "test-key",
			Model:               "gpt-test",
			MaxCompletionTokens: 512,
			TimeoutSeconds:      30,
		}},
		Context: config.ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Memory: config.MemoryConfig{
			Search: config.LayeredMemorySearchConfig{
				MemoryContextTokenBudget: 2000,
			},
			Semantic: config.MemorySemanticConfig{
				Bleve: config.BleveSemanticConfig{
					IndexName: "memory_records",
				},
				Embedding: config.EmbeddingProviderConfig{
					Provider:       "openai_compatible",
					Model:          "text-embedding-3-small",
					BaseURL:        "https://api.openai.com/v1",
					APIKey:         "test-key",
					Dimensions:     3,
					TimeoutSeconds: 30,
					BatchSize:      2,
				},
			},
		},
		Tools: config.ToolsConfig{
			Workspace: config.WorkspaceToolConfig{RootDir: "."},
			Mutation:  config.MutationToolConfig{RootDir: "."},
			RunCommand: config.RunCommandToolConfig{
				WorkDir: ".",
			},
		},
	}
}
