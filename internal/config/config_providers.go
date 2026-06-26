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
		if p.TimeoutSeconds <= 0 {
			return fmt.Errorf("provider %s: timeout_seconds must be > 0", name)
		}
		if p.MaxCompletionTokens <= 0 {
			return fmt.Errorf("provider %s: max_completion_tokens must be > 0", name)
		}
		if p.ReasoningEffort != "" {
			switch p.ReasoningEffort {
			case "low", "medium", "high":
			default:
				return fmt.Errorf("provider %s: reasoning_effort must be low, medium, or high, got %q", name, p.ReasoningEffort)
			}
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

// ContextPolicy returns the validated context configuration used by the
// context plane to derive token budgets and pressure thresholds. It validates
// the context fields and resolves the provider's output token cap so callers
// receive a complete, ready-to-use policy.
func (c *Config) ContextPolicy() (ContextConfig, error) {
	if c == nil {
		return ContextConfig{}, errors.New("config is required")
	}
	provider, err := c.EnabledProvider()
	if err != nil {
		return ContextConfig{}, fmt.Errorf("resolve context provider: %w", err)
	}
	if provider.MaxCompletionTokens <= 0 {
		return ContextConfig{}, fmt.Errorf("provider %s: max_completion_tokens must be > 0 for context policy", provider.Name)
	}
	if err := c.validateContext(); err != nil {
		return ContextConfig{}, err
	}
	return c.Context, nil
}
