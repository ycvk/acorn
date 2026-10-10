package memory

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

func (e *Engine) reserve(ctx context.Context, operation, model string, input, output int) (core.MemoryUsage, error) {
	scope := scopeFrom(ctx)
	now := e.cfg.Clock()
	u := core.MemoryUsage{ID: rand.Text(), Operation: operation, JobID: scope.jobID, RunID: scope.runID, Budget: scope.budget, Model: model, InputTokens: input, OutputTokens: output, CreatedAt: now}
	limit := 0
	switch scope.budget {
	case "memory":
		limit = e.cfg.DailyTokens
	case "autonomous":
		limit = e.cfg.AutonomousTokens
	}
	err := e.cfg.Store.ReserveMemoryUsage(ctx, u, limit, e.midnight(now))
	return u, err
}

func (e *Engine) generate(ctx context.Context, operation, instruction, input string, maxOutput int) (string, error) {
	n, err := e.cfg.Count(ctx, instruction+"\n"+input)
	if err != nil {
		return "", err
	}
	if n > e.cfg.BatchTokens {
		return "", fmt.Errorf("memory %s input uses %d tokens, batch limit is %d", operation, n, e.cfg.BatchTokens)
	}
	usage, err := e.reserve(ctx, operation, e.cfg.ModelName, n, maxOutput)
	if err != nil {
		return "", err
	}
	result, callErr := e.cfg.Model.GenerateMemory(ctx, instruction, input, maxOutput)
	if callErr == nil {
		usage.Reported = result.Reported
		if result.Reported {
			usage.InputTokens = result.InputTokens
			usage.OutputTokens = result.OutputTokens
		} else {
			usage.OutputTokens, err = e.cfg.Count(ctx, result.Text)
			if err != nil {
				return "", err
			}
		}
	}
	recordErr := e.cfg.Store.RecordMemoryUsage(context.WithoutCancel(ctx), usage)
	if err := errors.Join(callErr, recordErr); err != nil {
		return "", fmt.Errorf("memory %s: %w", operation, err)
	}
	return strings.TrimSpace(result.Text), nil
}

func (e *Engine) embed(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	n, err := e.cfg.Count(ctx, strings.Join(texts, "\n"))
	if err != nil {
		return nil, err
	}
	usage, err := e.reserve(ctx, "embedding_"+inputType, e.cfg.Index.Model, n, 0)
	if err != nil {
		return nil, err
	}
	reported := -1
	vectors, callErr := e.cfg.Embedder.EmbedStrings(ctx, texts, embeddingOptions(inputType, &reported))
	if reported >= 0 {
		usage.InputTokens = reported
		usage.Reported = true
	}
	recordErr := e.cfg.Store.RecordMemoryUsage(context.WithoutCancel(ctx), usage)
	if err := errors.Join(callErr, recordErr); err != nil {
		return nil, fmt.Errorf("memory embedding: %w", err)
	}
	return vectors, nil
}
