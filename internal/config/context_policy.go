package config

import "fmt"

const (
	defaultContextStaticOverheadTokens = 4096
	defaultContextWarningGapTokens     = 7000
	defaultContextTokenEncoding        = "o200k_base"
)

func (c *Config) ContextPolicy() (ContextPolicy, error) {
	if c == nil {
		return ContextPolicy{}, fmt.Errorf("config is required")
	}
	provider, err := c.EnabledProvider()
	if err != nil {
		return ContextPolicy{}, fmt.Errorf("resolve context provider: %w", err)
	}
	if provider.MaxCompletionTokens <= 0 {
		return ContextPolicy{}, fmt.Errorf("provider %s: max_completion_tokens must be > 0 for context policy", provider.Name)
	}
	return ContextPolicy{
		WindowTokens:         c.Context.WindowTokens,
		CompactMarginTokens:  c.Context.CompactMarginTokens,
		PreserveRecentTurns:  c.Context.PreserveRecentTurns,
		SummaryMaxTokens:     c.Context.SummaryMaxTokens,
		ReservedOutputTokens: provider.MaxCompletionTokens,
		TokenEncoding:        defaultContextTokenEncoding,
	}, nil
}
