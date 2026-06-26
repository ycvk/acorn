package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
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

func buildWebSearchTool(search WebSearchService, artifactService ArtifactService, bridge core.ToolCallContextBridge) (einotool.BaseTool, error) {
	if search == nil {
		return nil, errors.New("web search service is required")
	}
	if artifactService == nil {
		return nil, errors.New("artifact service is required for web_search")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required for web_search")
	}
	tool, err := inferProgressTool("web_search", "Search public web sources through the configured provider and persist the raw provider response.", func(ctx context.Context, input WebSearchInput, emit ToolProgressEmitter) (WebSearchOutput, error) {
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
		rawRecord, err := artifactService.WriteArtifact(ctx, core.ArtifactWriteRequest{
			RunID:               runID,
			SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
			SourceToolResultRef: sourceRef,
			Kind:                "json",
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

const defaultWebFetchPreviewBytes = 4000

type WebFetchInput struct {
	URL         string `json:"url" jsonschema:"required,description=HTTP or HTTPS URL to fetch through Acorn's outbound web policy."`
	ExtractMode string `json:"extract_mode,omitempty" jsonschema:"description=Extraction mode: auto, readability, full_page_markdown, or visible_text. Defaults to auto."`
}

type WebFetchOutput struct {
	URL                string               `json:"url"`
	FinalURL           string               `json:"final_url"`
	Status             int                  `json:"status"`
	ContentType        string               `json:"content_type"`
	ContentLength      int64                `json:"content_length"`
	FetchedAt          string               `json:"fetched_at"`
	Redirects          []string             `json:"redirects,omitempty"`
	Title              string               `json:"title,omitempty"`
	SiteName           string               `json:"site_name,omitempty"`
	PublishedTime      string               `json:"published_time,omitempty"`
	ExtractionMethod   string               `json:"extraction_method"`
	ExtractionWarning  string               `json:"extraction_warning,omitempty"`
	MarkdownPreview    string               `json:"markdown_preview"`
	MarkdownTruncated  bool                 `json:"markdown_truncated,omitempty"`
	RawSHA256          string               `json:"raw_sha256"`
	RawArtifactID      string               `json:"raw_artifact_id"`
	MarkdownArtifactID string               `json:"markdown_artifact_id"`
	RawArtifact        ArtifactSummary      `json:"raw_artifact"`
	MarkdownArtifact   ArtifactSummary      `json:"markdown_artifact"`
	Links              []webaccess.PageLink `json:"links,omitempty"`
}

func buildWebFetchTool(fetcher WebFetchService, artifactService ArtifactService, bridge core.ToolCallContextBridge) (einotool.BaseTool, error) {
	if fetcher == nil {
		return nil, errors.New("web fetch service is required")
	}
	if artifactService == nil {
		return nil, errors.New("artifact service is required for web_fetch")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required for web_fetch")
	}
	tool, err := inferProgressTool("web_fetch", "Fetch a public HTTP(S) URL, extract Markdown, and persist raw/Markdown artifacts.", func(ctx context.Context, input WebFetchInput, emit ToolProgressEmitter) (WebFetchOutput, error) {
		runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
		if runID == "" {
			return WebFetchOutput{}, errors.New("web_fetch requires current run context")
		}
		callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
		if callID == "" {
			return WebFetchOutput{}, errors.New("web_fetch requires current tool call context")
		}
		sourceRef := "tool_result:" + runID + ":" + callID
		result, err := fetcher.Fetch(ctx, webaccess.FetchRequest{
			URL:         input.URL,
			ExtractMode: webaccess.ExtractionMode(strings.TrimSpace(input.ExtractMode)),
		})
		if err != nil {
			return WebFetchOutput{}, err
		}
		sessionID := strings.TrimSpace(bridge.CurrentSessionID(ctx))
		rawRecord, err := artifactService.WriteArtifact(ctx, core.ArtifactWriteRequest{
			RunID:               runID,
			SessionID:           sessionID,
			SourceToolResultRef: sourceRef,
			Kind:                "text",
			Title:               artifactTitle("web_fetch raw", result.Extracted.Title, result.FinalURL),
			MIMEType:            result.ContentType,
			Content:             result.Raw,
		})
		if err != nil {
			return WebFetchOutput{}, err
		}
		markdownRecord, err := artifactService.WriteArtifact(ctx, core.ArtifactWriteRequest{
			RunID:               runID,
			SessionID:           sessionID,
			SourceToolResultRef: sourceRef,
			Kind:                "markdown",
			Title:               artifactTitle("web_fetch markdown", result.Extracted.Title, result.FinalURL),
			MIMEType:            "text/markdown; charset=utf-8",
			Content:             []byte(result.Extracted.Markdown),
		})
		if err != nil {
			return WebFetchOutput{}, err
		}
		preview, truncated := previewBytes([]byte(result.Extracted.Markdown), defaultWebFetchPreviewBytes)
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("fetched %s (%d bytes, %s)", result.FinalURL, result.ContentLength, result.Extracted.Method)); err != nil {
			return WebFetchOutput{}, err
		}
		return WebFetchOutput{
			URL:                result.URL,
			FinalURL:           result.FinalURL,
			Status:             result.Status,
			ContentType:        result.ContentType,
			ContentLength:      result.ContentLength,
			FetchedAt:          result.FetchedAt.Format("2006-01-02T15:04:05.000000000Z07:00"),
			Redirects:          append([]string(nil), result.Redirects...),
			Title:              result.Extracted.Title,
			SiteName:           result.Extracted.SiteName,
			PublishedTime:      result.Extracted.PublishedTime,
			ExtractionMethod:   result.Extracted.Method,
			ExtractionWarning:  result.Extracted.Warning,
			MarkdownPreview:    preview,
			MarkdownTruncated:  truncated,
			RawSHA256:          result.RawSHA256,
			RawArtifactID:      rawRecord.ArtifactID,
			MarkdownArtifactID: markdownRecord.ArtifactID,
			RawArtifact:        artifactSummaryFromRecord(rawRecord),
			MarkdownArtifact:   artifactSummaryFromRecord(markdownRecord),
			Links:              append([]webaccess.PageLink(nil), result.Extracted.Links...),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build web_fetch tool: %w", err)
	}
	return tool, nil
}

func artifactTitle(prefix, title, fallbackURL string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(fallbackURL)
	}
	if title == "" {
		return prefix
	}
	return prefix + ": " + title
}
