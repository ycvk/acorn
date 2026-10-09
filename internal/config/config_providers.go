package config

import (
	"errors"
	"fmt"
	"strings"
)

func (c *Config) validateProviders() error {
	var enabled []ProviderConfig
	for _, p := range c.Providers {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 0 {
		return errors.New("at least one provider must be enabled")
	}
	seenNames := make(map[string]struct{}, len(enabled))
	for _, p := range enabled {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return errors.New("provider name is required")
		}
		if _, ok := seenNames[name]; ok {
			return fmt.Errorf("duplicate enabled provider name %q", name)
		}
		seenNames[name] = struct{}{}
		if strings.TrimSpace(p.Model) == "" {
			return fmt.Errorf("provider %s: model is required", name)
		}
		if strings.TrimSpace(p.BaseURL) == "" {
			return fmt.Errorf("provider %s: base_url is required", name)
		}
		if strings.TrimSpace(p.APIKey) == "" {
			return fmt.Errorf("provider %s: api_key is required — set it in the config or export the env var it references (the example config uses ${OPENAI_API_KEY}; self-hosted reads it from ~/.acorn/acorn.env)", name)
		}
		if err := p.validateModelSettings(); err != nil {
			return fmt.Errorf("provider %s: %w", name, err)
		}
	}
	if len(enabled) > 1 {
		return fmt.Errorf("exactly one provider must be enabled, got %d", len(enabled))
	}
	return nil
}

func (c *Config) EnabledProvider() (ProviderConfig, error) {
	var enabled []ProviderConfig
	for _, p := range c.Providers {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 0 {
		return ProviderConfig{}, errors.New("no enabled providers")
	}
	if len(enabled) > 1 {
		return ProviderConfig{}, fmt.Errorf("exactly one provider must be enabled, got %d", len(enabled))
	}
	return enabled[0], nil
}

// ContextPolicy returns the configured context budgets after validation.
func (c *Config) ContextPolicy() (ContextConfig, error) {
	if c == nil {
		return ContextConfig{}, errors.New("config is required")
	}
	if _, err := c.InputTokenBudget(); err != nil {
		return ContextConfig{}, err
	}
	return c.Context, nil
}

// InputTokenBudget reserves generated output and the ephemeral presence block
// before applying the additional compaction margin.
func (c *Config) InputTokenBudget() (int, error) {
	if c == nil {
		return 0, errors.New("config is required")
	}
	if err := c.validateContext(); err != nil {
		return 0, err
	}
	p, err := c.EnabledProvider()
	if err != nil {
		return 0, err
	}
	limits := modelLimitsFor(p.Model)
	if limits.context > 0 && c.Context.WindowTokens > limits.context {
		return 0, fmt.Errorf("context.window_tokens exceeds %s context window (%d)", p.Model, limits.context)
	}
	output := limits.output
	if p.MaxOutputTokens != nil {
		output = *p.MaxOutputTokens
	}
	budget := c.Context.WindowTokens - c.Context.CompactMarginTokens - c.Presence.MaxTokens - output
	if budget <= 0 {
		return 0, errors.New("context.window_tokens must leave input space after output, presence and compact_margin_tokens")
	}
	return budget, nil
}
