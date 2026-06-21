package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// TestNewContainerStartsWithoutSemanticConfig verifies missing semantic config no
// longer blocks startup: the container builds without a semantic runtime, and
// Search/Prepare fail loud only when actually used (not at construction).
func TestNewContainerStartsWithoutSemanticConfig(t *testing.T) {
	cfg := testContainerConfig(t)
	cfg.Memory.Semantic = config.MemorySemanticConfig{}

	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewContainer should succeed without semantic config: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
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
		},
		Memory: config.MemoryConfig{
			Search: config.MemorySearchConfig{
				MemoryContextTokenBudget: 2000,
			},
			Semantic: config.MemorySemanticConfig{
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
