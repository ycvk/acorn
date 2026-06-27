package tools

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// RunSearchStore is the subset of core.SessionStore that search_runs needs.
type RunSearchStore interface {
	SearchRuns(ctx context.Context, query string, limit int) ([]core.RunRecord, error)
}

// SearchRunsInput is the tool input for search_runs.
type SearchRunsInput struct {
	Query string `json:"query" jsonschema:"description=Keyword to search for in past run inputs."`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Maximum results to return (default 10)."`
}

// SearchRunsOutput is the tool result.
type SearchRunsOutput struct {
	Runs []RunSummary `json:"runs"`
}

// RunSummary is a compact run record for the model.
type RunSummary struct {
	RunID     string `json:"run_id"`
	Input     string `json:"input"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func buildSearchRunsTool(store RunSearchStore) (einotool.BaseTool, error) {
	if store == nil {
		return nil, fmt.Errorf("search_runs requires a run search store")
	}
	tool, err := inferProgressTool("search_runs", "Search past run histories by keyword. Returns matching runs with their input, status, and creation time. Use this to recall what you or previous runs did.", func(ctx context.Context, input SearchRunsInput, emit ToolProgressEmitter) (SearchRunsOutput, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return SearchRunsOutput{}, fmt.Errorf("search_runs query is required")
		}
		limit := input.Limit
		if limit <= 0 {
			limit = 10
		}
		runs, err := store.SearchRuns(ctx, query, limit)
		if err != nil {
			return SearchRunsOutput{}, fmt.Errorf("search_runs: %w", err)
		}
		summaries := make([]RunSummary, 0, len(runs))
		for _, r := range runs {
			summaries = append(summaries, RunSummary{
				RunID:     r.RunID,
				Input:     r.Input,
				Status:    string(r.Status),
				CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		return SearchRunsOutput{Runs: summaries}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build search_runs tool: %w", err)
	}
	return tool, nil
}
