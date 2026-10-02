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
			MaskAfterTurns:      2,
		},
		Approval: ApprovalConfig{Require: []string{"browser", "mcp__*"}},
		Owner:    OwnerConfig{Timezone: "UTC"},
		Presence: PresenceConfig{MaxTokens: 4000},
		Wake:     WakeConfig{DailyLimit: 20},
		Notify: NotifyConfig{
			MaxPerHour: 6,
			QuietHours: QuietHoursConfig{Start: "23:00", End: "08:00"},
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
			Name:          "coordinator",
			Description:   "A personal agent that works on its owner's behalf.",
			MaxIterations: 70,
		},
		Tools: ToolsConfig{
			Workspace: WorkspaceToolConfig{RootDir: "."},
		},
		Memory: MemoryConfig{
			Search: MemorySearchConfig{
				MemoryContextTokenBudget: 8000,
			},
			Embedding: MemoryEmbeddingConfig{
				Enabled:    false,
				Model:      "text-embedding-3-small",
				Dimensions: 1536,
			},
			Review: MemoryReviewConfig{
				ReviewInterval: 5,
			},
			Active: MemoryActiveConfig{
				CharLimit: 2200,
			},
		},
	}
}
