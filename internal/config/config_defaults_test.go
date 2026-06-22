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
		{"web_access.user_agent", cfg.WebAccess.UserAgent, "Acorn/0.x (+https://github.com/ycvk/acorn)"},
		{"web_access.timeout_seconds", cfg.WebAccess.TimeoutSeconds, 20},
		{"web_access.max_response_bytes", cfg.WebAccess.MaxResponseBytes, int64(10 * 1024 * 1024)},
		{"web_access.allow_private_networks", cfg.WebAccess.AllowPrivateNetworks, false},
		{"web_access.search.provider", cfg.WebAccess.Search.Provider, "tavily"},
		{"web_access.search.timeout_seconds", cfg.WebAccess.Search.TimeoutSeconds, 10},
		{"web_access.search.max_results", cfg.WebAccess.Search.MaxResults, 10},
		{"browser.headless", cfg.Browser.Headless, true},
		{"browser.default_timeout_seconds", cfg.Browser.DefaultTimeoutSeconds, 20},
		{"context.window_tokens", cfg.Context.WindowTokens, 200000},
		{"context.compact_margin_tokens", cfg.Context.CompactMarginTokens, 13000},
		{"context.preserve_recent_turns", cfg.Context.PreserveRecentTurns, 3},
		{"context.mask_after_turns", cfg.Context.MaskAfterTurns, 2},
		{"agent.max_iterations", cfg.Agent.MaxIterations, 70},
		{"memory.semantic.embedding.provider", cfg.Memory.Semantic.Embedding.Provider, "openai_compatible"},
		{"memory.semantic.embedding.model", cfg.Memory.Semantic.Embedding.Model, ""},
		{"memory.semantic.embedding.base_url", cfg.Memory.Semantic.Embedding.BaseURL, ""},
		{"memory.semantic.embedding.api_key", cfg.Memory.Semantic.Embedding.APIKey, "${OPENAI_API_KEY}"},
		{"memory.semantic.embedding.dimensions", cfg.Memory.Semantic.Embedding.Dimensions, 1536},
		{"memory.semantic.embedding.timeout_seconds", cfg.Memory.Semantic.Embedding.TimeoutSeconds, 30},
		{"memory.semantic.embedding.batch_size", cfg.Memory.Semantic.Embedding.BatchSize, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
