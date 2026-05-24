package web

import "github.com/ycvk/acorn/internal/config"

// ClientProviderSettingsDTO represents a provider configuration exposed to the client.
type ClientProviderSettingsDTO struct {
	Name                string  `json:"name"`
	Model               string  `json:"model"`
	BaseURL             string  `json:"base_url,omitempty"`
	ReasoningEffort     string  `json:"reasoning_effort,omitempty"`
	TimeoutSeconds      int     `json:"timeout_seconds,omitempty"`
	Temperature         float32 `json:"temperature,omitempty"`
	MaxCompletionTokens int     `json:"max_completion_tokens,omitempty"`
	Enabled             bool    `json:"enabled"`
}

// ClientRuntimeSettingsDTO represents runtime configuration exposed to the client.
type ClientRuntimeSettingsDTO struct {
	StorageDir        string `json:"storage_dir"`
	RunTimeoutSeconds int    `json:"run_timeout_seconds"`
}

// ClientWebSettingsDTO represents web server configuration exposed to the client.
type ClientWebSettingsDTO struct {
	ListenAddr     string   `json:"listen_addr"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

// ClientSettingsDTO aggregates all client-visible settings.
type ClientSettingsDTO struct {
	ConfigPath    string                      `json:"config_path,omitempty"`
	ConfigDir     string                      `json:"config_dir,omitempty"`
	WorkspaceRoot string                      `json:"workspace_root"`
	Providers     []ClientProviderSettingsDTO `json:"providers"`
	Runtime       ClientRuntimeSettingsDTO    `json:"runtime"`
	Web           ClientWebSettingsDTO        `json:"web"`
}

func clientSettingsDTOFromConfig(cfg *config.Config) ClientSettingsDTO {
	providers := make([]ClientProviderSettingsDTO, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		providers = append(providers, ClientProviderSettingsDTO{
			Name:                p.Name,
			Model:               p.Model,
			BaseURL:             p.BaseURL,
			ReasoningEffort:     p.ReasoningEffort,
			TimeoutSeconds:      p.TimeoutSeconds,
			Temperature:         p.Temperature,
			MaxCompletionTokens: p.MaxCompletionTokens,
			Enabled:             p.Enabled,
		})
	}
	return ClientSettingsDTO{
		ConfigPath:    cfg.ConfigPath,
		ConfigDir:     cfg.ConfigDir,
		WorkspaceRoot: cfg.WorkspaceRoot(),
		Providers:     providers,
		Runtime: ClientRuntimeSettingsDTO{
			StorageDir:        cfg.Runtime.StorageDir,
			RunTimeoutSeconds: cfg.Runtime.RunTimeoutSeconds,
		},
		Web: ClientWebSettingsDTO{
			ListenAddr:     cfg.Web.ListenAddr,
			AllowedOrigins: append([]string(nil), cfg.Web.AllowedOrigins...),
		},
	}
}
