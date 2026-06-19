package config

// DefaultConfig returns a Config with sensible defaults for testing.
func DefaultConfig() *Config {
	return defaultConfig()
}

func defaultConfig() *Config {
	return &Config{
		Providers: []ProviderConfig{
			{
				Name:                "default",
				Model:               "gpt-4o-mini",
				BaseURL:             "https://api.openai.com/v1",
				APIKey:              "",
				TimeoutSeconds:      30,
				Temperature:         0.1,
				MaxCompletionTokens: 2048,
				Enabled:             true,
			},
		},
		Context: ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Runtime: RuntimeConfig{
			StorageDir:        "~/.acorn",
			RunTimeoutSeconds: 900,
		},
		Web: WebConfig{ListenAddr: "127.0.0.1:8080"},
		WebAccess: WebAccessConfig{
			UserAgent:            "Acorn/0.x (+https://github.com/ycvk/acorn)",
			TimeoutSeconds:       20,
			MaxResponseBytes:     10 * 1024 * 1024,
			AllowPrivateNetworks: false,
			Search: WebSearchConfig{
				Provider:       "tavily",
				APIKey:         "${TAVILY_API_KEY}",
				TimeoutSeconds: 10,
				MaxResults:     10,
			},
		},
		Browser: BrowserConfig{
			ExecutablePath:        "",
			Headless:              true,
			DefaultTimeoutSeconds: 20,
		},
		Agent: AgentConfig{
			Name:             "coordinator",
			Description:      "A local operator agent that can inspect files and execute commands.",
			MaxIterations:    70,
			MaxSubagentDepth: 3,
		},
		Tools: ToolsConfig{
			Workspace:  WorkspaceToolConfig{RootDir: "."},
			Mutation:   MutationToolConfig{},
			RunCommand: RunCommandToolConfig{DefaultTimeout: 30, WorkDir: "."},
		},
		Memory: MemoryConfig{
			Search: MemorySearchConfig{
				MemoryContextTokenBudget: 2000,
			},
			Semantic: MemorySemanticConfig{
				Bleve: BleveSemanticConfig{
					Path:      "",
					IndexName: "memory_records",
				},
				Embedding: EmbeddingProviderConfig{
					Provider:       "openai_compatible",
					Model:          "text-embedding-3-small",
					BaseURL:        "https://api.openai.com/v1",
					APIKey:         "${OPENAI_API_KEY}",
					Dimensions:     1536,
					TimeoutSeconds: 30,
					BatchSize:      64,
				},
			},
		},
	}
}
