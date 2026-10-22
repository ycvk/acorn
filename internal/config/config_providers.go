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

// The parts of the context window kept out of the conversation.
const (
	// CompactMarginTokens is kept free when older turns are summarized.
	CompactMarginTokens = 32000
	// PresenceMaxTokens caps the <presence> block added before each call.
	PresenceMaxTokens = 4000
)

// ContextWindow is the enabled model's context window: the model table's for
// a known model, context_window_tokens for any other.
func (c *Config) ContextWindow() (int, error) {
	if c == nil {
		return 0, errors.New("config is required")
	}
	p, err := c.EnabledProvider()
	if err != nil {
		return 0, err
	}
	if limits := modelLimitsFor(p.Model); limits.context > 0 {
		return limits.context, nil
	}
	if p.ContextWindowTokens == nil {
		return 0, fmt.Errorf("provider %s: context_window_tokens is required for %s", p.Name, p.Model)
	}
	return *p.ContextWindowTokens, nil
}

// InputTokenBudget reserves generated output and the ephemeral presence block
// before applying the additional compaction margin.
func (c *Config) InputTokenBudget() (int, error) {
	window, err := c.ContextWindow()
	if err != nil {
		return 0, err
	}
	p, err := c.EnabledProvider()
	if err != nil {
		return 0, err
	}
	output := modelLimitsFor(p.Model).output
	if p.MaxOutputTokens != nil {
		output = *p.MaxOutputTokens
	}
	budget := window - CompactMarginTokens - PresenceMaxTokens - output
	if budget <= 0 {
		return 0, fmt.Errorf("the %d token context window of %s leaves no input space after output, presence and the compaction margin", window, p.Model)
	}
	return budget, nil
}
