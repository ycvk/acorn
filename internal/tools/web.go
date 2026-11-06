package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

type WebSearchInput struct {
	Query       string `json:"query" jsonschema:"required" jsonschema_description:"Search query for public web source discovery."`
	MaxResults  int    `json:"max_results,omitempty" jsonschema_description:"Maximum returned results. Defaults to configured max_results."`
	TimeRange   string `json:"time_range,omitempty" jsonschema_description:"Optional published-date window: day, week, month, or year."`
	IncludeText bool   `json:"include_text,omitempty" jsonschema_description:"Also return each result's page text, cut at 3000 characters, so a simple question can be answered without web_fetch. Keep max_results small when set."`
}

type WebSearchOutput struct {
	Query           string                           `json:"query"`
	Provider        string                           `json:"provider"`
	SearchedAt      string                           `json:"searched_at"`
	Results         []webaccess.SearchResultItem     `json:"results"`
	FilteredResults []webaccess.FilteredSearchResult `json:"filtered_results,omitempty"`
	RawArtifactID   string                           `json:"raw_artifact_id"`
}

func buildWebSearchTool(search WebSearchService, artifactService core.ArtifactService, bridge core.ToolCallContextBridge) (einotool.BaseTool, error) {
	if search == nil {
		return nil, errors.New("web search service is required")
	}
	if artifactService == nil {
		return nil, errors.New("artifact service is required for web_search")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required for web_search")
	}
	tool, err := inferLocalTool("web_search", "Search public web sources through the configured provider and persist the raw provider response.", func(ctx context.Context, input WebSearchInput) (WebSearchOutput, error) {
		runID, sourceRef, err := toolResultRef(ctx, bridge, "web_search")
		if err != nil {
			return WebSearchOutput{}, err
		}
		result, err := search.Search(ctx, webaccess.SearchRequest{
			Query:       input.Query,
			MaxResults:  input.MaxResults,
			TimeRange:   input.TimeRange,
			IncludeText: input.IncludeText,
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
		return WebSearchOutput{
			Query:           result.Query,
			Provider:        result.Provider,
			SearchedAt:      result.SearchedAt.Format(toolTimeLayout),
			Results:         append([]webaccess.SearchResultItem(nil), result.Results...),
			FilteredResults: append([]webaccess.FilteredSearchResult(nil), result.FilteredResults...),
			RawArtifactID:   rawRecord.ArtifactID,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build web_search tool: %w", err)
	}
	return tool, nil
}

// webFetchInlineBytes is how much of a page's Markdown web_fetch returns in
// its result. Most article pages fit, so answering from a page seldom takes
// another step with artifact_read.
const webFetchInlineBytes = 16000

type WebFetchInput struct {
	URL         string `json:"url" jsonschema:"required" jsonschema_description:"HTTP or HTTPS URL to fetch through Acorn's outbound web policy."`
	ExtractMode string `json:"extract_mode,omitempty" jsonschema_description:"Extraction mode: auto, readability, full_page_markdown, or visible_text. Defaults to auto, the article with its links; full_page_markdown keeps the whole page, navigation included."`
}

type WebFetchOutput struct {
	URL                string   `json:"url"`
	FinalURL           string   `json:"final_url"`
	Status             int      `json:"status"`
	ContentType        string   `json:"content_type"`
	ContentLength      int64    `json:"content_length"`
	FetchedAt          string   `json:"fetched_at"`
	Redirects          []string `json:"redirects,omitempty"`
	Title              string   `json:"title,omitempty"`
	SiteName           string   `json:"site_name,omitempty"`
	PublishedTime      string   `json:"published_time,omitempty"`
	ExtractionMethod   string   `json:"extraction_method"`
	ExtractionWarning  string   `json:"extraction_warning,omitempty"`
	Markdown           string   `json:"markdown"`
	MarkdownBytes      int      `json:"markdown_bytes"`
	NextOffset         int      `json:"next_offset,omitempty"`
	RawSHA256          string   `json:"raw_sha256"`
	RawArtifactID      string   `json:"raw_artifact_id"`
	MarkdownArtifactID string   `json:"markdown_artifact_id"`
}

func buildWebFetchTool(fetcher WebFetchService, artifactService core.ArtifactService, bridge core.ToolCallContextBridge) (einotool.BaseTool, error) {
	if fetcher == nil {
		return nil, errors.New("web fetch service is required")
	}
	if artifactService == nil {
		return nil, errors.New("artifact service is required for web_fetch")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required for web_fetch")
	}
	tool, err := inferLocalTool("web_fetch", "Fetch a public HTTP(S) URL (HTML, plain text, XML or JSON) and return it as Markdown, the first 16000 bytes in the result. A longer page sets next_offset: artifact_read on markdown_artifact_id from that offset reads the rest. An RSS, Atom or JSON feed comes back as its entries with title, link, date and summary.", func(ctx context.Context, input WebFetchInput) (WebFetchOutput, error) {
		runID, sourceRef, err := toolResultRef(ctx, bridge, "web_fetch")
		if err != nil {
			return WebFetchOutput{}, err
		}
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
		markdown, nextOffset := inlineMarkdown(result.Extracted.Markdown, webFetchInlineBytes)
		return WebFetchOutput{
			URL:                result.URL,
			FinalURL:           result.FinalURL,
			Status:             result.Status,
			ContentType:        result.ContentType,
			ContentLength:      result.ContentLength,
			FetchedAt:          result.FetchedAt.Format(toolTimeLayout),
			Redirects:          append([]string(nil), result.Redirects...),
			Title:              result.Extracted.Title,
			SiteName:           result.Extracted.SiteName,
			PublishedTime:      result.Extracted.PublishedTime,
			ExtractionMethod:   result.Extracted.Method,
			ExtractionWarning:  result.Extracted.Warning,
			Markdown:           markdown,
			MarkdownBytes:      len(result.Extracted.Markdown),
			NextOffset:         nextOffset,
			RawSHA256:          result.RawSHA256,
			RawArtifactID:      rawRecord.ArtifactID,
			MarkdownArtifactID: markdownRecord.ArtifactID,
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

// inlineMarkdown returns at most limit bytes of markdown, cut before a
// character, and the byte offset where the rest begins, zero when nothing was
// cut.
func inlineMarkdown(markdown string, limit int) (string, int) {
	if len(markdown) <= limit {
		return markdown, 0
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(markdown[cut]) {
		cut--
	}
	return markdown[:cut], cut
}
