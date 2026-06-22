package app

import (
	"fmt"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
)

func buildContextPlane(cfg *config.Config) (contextplane.ContextPlane, error) {
	contextCounter, err := contextplane.NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("context plane token counter: %w", err)
	}
	maxContextTokens := cfg.Context.WindowTokens - cfg.Context.CompactMarginTokens
	if maxContextTokens <= 0 {
		return nil, fmt.Errorf("context effective window must be positive: window=%d margin=%d", cfg.Context.WindowTokens, cfg.Context.CompactMarginTokens)
	}
	contextPlane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: cfg.Memory.Search.MemoryContextTokenBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             contextCounter,
	})
	return contextPlane, nil
}
