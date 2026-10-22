package config

// DefaultConfig returns a Config with sensible defaults for testing.
func DefaultConfig() *Config {
	return defaultConfig()
}

func defaultConfig() *Config {
	return &Config{
		Providers: []ProviderConfig{
			{
				Name:           "default",
				API:            "responses",
				Model:          "gpt-6-astra",
				BaseURL:        "https://api.openai.com/v1",
				APIKey:         "",
				TimeoutSeconds: 0,
				Enabled:        true,
			},
		},
		Memory: MemoryConfig{Embedding: MemoryEmbeddingConfig{BaseURL: "https://api.voyageai.com/v1", APIKey: "${VOYAGE_API_KEY}", Model: "voyage-4", Dimensions: 1024}},
		Backup: BackupConfig{Keep: 14},
		Review: ReviewConfig{
			APIKey:         "${TYPESAFE_API_KEY}",
			Model:          "jev-preview",
			TimeoutSeconds: 20,
		},
		Workstation: WorkstationConfig{Token: "${ACORN_WORKSTATION_TOKEN}"},
		Files:       FilesConfig{MaxUploadMiB: 50, MaxSendMiB: 100},
		Runtime:     RuntimeConfig{StorageDir: "~/.acorn"},
		Web:         WebConfig{ListenAddr: "127.0.0.1:8080"},
		WebAccess: WebAccessConfig{
			AllowPrivateNetworks: false,
			Search:               WebSearchConfig{Provider: "exa", APIKey: "${EXA_API_KEY}"},
		},
	}
}
