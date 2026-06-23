package toolset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolkit"
	"github.com/ycvk/acorn/internal/webaccess"
)

type WebSearchInput struct {
	Query      string `json:"query" jsonschema:"required,description=Search query for public web source discovery."`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"description=Maximum returned results. Defaults to configured max_results."`
	TimeRange  string `json:"time_range,omitempty" jsonschema:"description=Optional provider time range such as day, week, month, or year."`
}

type WebSearchOutput struct {
	Query           string                           `json:"query"`
	Provider        string                           `json:"provider"`
	SearchedAt      string                           `json:"searched_at"`
	Results         []webaccess.SearchResultItem     `json:"results"`
	FilteredResults []webaccess.FilteredSearchResult `json:"filtered_results,omitempty"`
	RawArtifactID   string                           `json:"raw_artifact_id"`
	RawArtifact     ArtifactSummary                  `json:"raw_artifact"`
	ResponseTime    float64                          `json:"response_time,omitempty"`
}

func buildWebSearchTool(search WebSearchService, artifactService ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	if search == nil {
		return nil, errors.New("web search service is required")
	}
	if artifactService == nil {
		return nil, errors.New("artifact service is required for web_search")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required for web_search")
	}
	tool, err := inferProgressTool("web_search", "Search public web sources through the configured provider and persist the raw provider response.", func(ctx context.Context, input WebSearchInput, emit toolkit.ToolProgressEmitter) (WebSearchOutput, error) {
		runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
		if runID == "" {
			return WebSearchOutput{}, errors.New("web_search requires current run context")
		}
		callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
		if callID == "" {
			return WebSearchOutput{}, errors.New("web_search requires current tool call context")
		}
		sourceRef := "tool_result:" + runID + ":" + callID
		result, err := search.Search(ctx, webaccess.SearchRequest{
			Query:      input.Query,
			MaxResults: input.MaxResults,
			TimeRange:  input.TimeRange,
		})
		if err != nil {
			return WebSearchOutput{}, err
		}
		rawRecord, err := artifactService.Write(ctx, store.ArtifactWriteRequest{
			RunID:               runID,
			SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
			SourceToolResultRef: sourceRef,
			Kind:                store.ArtifactKindJSON,
			Title:               "web_search raw: " + result.Query,
			MIMEType:            "application/json",
			Content:             result.Raw,
		})
		if err != nil {
			return WebSearchOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("searched web with %s: %d result(s), %d filter group(s)", result.Provider, len(result.Results), len(result.FilteredResults))); err != nil {
			return WebSearchOutput{}, err
		}
		return WebSearchOutput{
			Query:           result.Query,
			Provider:        result.Provider,
			SearchedAt:      result.SearchedAt.Format("2006-01-02T15:04:05.000000000Z07:00"),
			Results:         append([]webaccess.SearchResultItem(nil), result.Results...),
			FilteredResults: append([]webaccess.FilteredSearchResult(nil), result.FilteredResults...),
			RawArtifactID:   rawRecord.ArtifactID,
			RawArtifact:     artifactSummaryFromRecord(rawRecord),
			ResponseTime:    result.ResponseTime,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build web_search tool: %w", err)
	}
	return tool, nil
}
