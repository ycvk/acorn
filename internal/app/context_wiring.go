package app

import (
	"fmt"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/workingstate"
)

func buildContextPlane(cfg *config.Config, store containerRuntimeStore, checkpointService *workingstate.Service, sessionSummaryService *model.SessionSummaryService) (contextplane.ContextPlane, error) {
	contextPolicy, err := cfg.ContextPolicy()
	if err != nil {
		return nil, fmt.Errorf("context policy: %w", err)
	}
	maxContextTokens, err := contextplane.ContextAssemblyTokenLimitFromContextPolicy(contextPolicy)
	if err != nil {
		return nil, fmt.Errorf("context plane budget: %w", err)
	}
	contextCounter, err := contextplane.NewCompressionTokenCounter(contextPolicy)
	if err != nil {
		return nil, fmt.Errorf("context plane token counter: %w", err)
	}
	contextPlane := contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: cfg.Memory.Search.MemoryContextTokenBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             contextCounter,
		Store:                    store,
		CheckpointService:        checkpointService,
		SessionSummaryService:    sessionSummaryService,
		ToolResultLedger:         store,
	})
	return contextPlane, nil
}
