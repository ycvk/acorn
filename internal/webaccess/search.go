package webaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultExaSearchURL = "https://api.exa.ai/search"

// SearchTextMaxCharacters caps the page text returned per result when a
// search asks for it, so a full result list stays within one tool result.
const SearchTextMaxCharacters = 3000

type SearchConfig struct {
	APIKey           string
	Timeout          time.Duration
	MaxResults       int
	MaxResponseBytes int64
	Policy           URLPolicy
	HTTPClient       *http.Client
	SearchURL        string
	Now              func() time.Time
}

type SearchService struct {
	apiKey           string
	timeout          time.Duration
	maxResults       int
	maxResponseBytes int64
	policy           URLPolicy
	httpClient       *http.Client
	searchURL        string
	now              func() time.Time
}

type SearchRequest struct {
	Query       string
	MaxResults  int
	TimeRange   string
	IncludeText bool
}

type SearchResultItem struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Snippet       string `json:"snippet"`
	PublishedDate string `json:"published_date,omitempty"`
	Rank          int    `json:"rank"`
	Text          string `json:"text,omitempty"`
}

type FilteredSearchResult struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type SearchResult struct {
	Query           string
	Provider        string
	SearchedAt      time.Time
	Results         []SearchResultItem
	FilteredResults []FilteredSearchResult
	Raw             []byte
}

func NewSearchService(cfg SearchConfig) (*SearchService, error) {
	if cfg.Timeout <= 0 {
		return nil, errors.New("web search timeout must be > 0")
	}
	if cfg.MaxResults <= 0 {
		return nil, errors.New("web search max results must be > 0")
	}
	if cfg.MaxResponseBytes <= 0 {
		return nil, errors.New("web search max response bytes must be > 0")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	searchURL := strings.TrimSpace(cfg.SearchURL)
	if searchURL == "" {
		searchURL = defaultExaSearchURL
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SearchService{
		apiKey:           strings.TrimSpace(cfg.APIKey),
		timeout:          cfg.Timeout,
		maxResults:       cfg.MaxResults,
		maxResponseBytes: cfg.MaxResponseBytes,
		policy:           cfg.Policy,
		httpClient:       client,
		searchURL:        searchURL,
		now:              now,
	}, nil
}

func (s *SearchService) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	if s == nil {
		return SearchResult{}, errors.New("web search service is nil")
	}
	if strings.TrimSpace(s.apiKey) == "" {
		return SearchResult{}, errors.New("web_access.search.api_key is required")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return SearchResult{}, errors.New("query is required")
	}
	maxResults := req.MaxResults
	if maxResults <= 0 || maxResults > s.maxResults {
		maxResults = s.maxResults
	}
	startPublished, err := exaStartPublishedDate(req.TimeRange, s.now())
	if err != nil {
		return SearchResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	contents := exaContents{Highlights: true}
	if req.IncludeText {
		contents.Text = &exaTextOptions{MaxCharacters: SearchTextMaxCharacters}
	}
	body, err := json.Marshal(exaSearchRequest{
		Query:              query,
		NumResults:         maxResults,
		StartPublishedDate: startPublished,
		Contents:           contents,
	})
	if err != nil {
		return SearchResult{}, fmt.Errorf("marshal Exa search request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.searchURL, bytes.NewReader(body))
	if err != nil {
		return SearchResult{}, fmt.Errorf("create Exa search request: %w", err)
	}
	httpReq.Header.Set("x-api-key", s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	client := *s.httpClient
	client.Timeout = s.timeout
	resp, err := client.Do(httpReq)
	if err != nil {
		return SearchResult{}, fmt.Errorf("exa search request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body, s.maxResponseBytes)
	if err != nil {
		return SearchResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("exa search returned HTTP status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded exaSearchResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return SearchResult{}, fmt.Errorf("decode Exa search response: %w", err)
	}
	results, filtered := s.filterResults(ctx, decoded.Results)
	return SearchResult{
		Query:           query,
		Provider:        "exa",
		SearchedAt:      s.now().UTC(),
		Results:         results,
		FilteredResults: filtered,
		Raw:             raw,
	}, nil
}

// exaStartPublishedDate turns the tool's time range into the lower bound of
// Exa's published-date filter; an empty range searches all dates.
func exaStartPublishedDate(timeRange string, now time.Time) (string, error) {
	now = now.UTC()
	var start time.Time
	switch strings.TrimSpace(timeRange) {
	case "":
		return "", nil
	case "day":
		start = now.AddDate(0, 0, -1)
	case "week":
		start = now.AddDate(0, 0, -7)
	case "month":
		start = now.AddDate(0, -1, 0)
	case "year":
		start = now.AddDate(-1, 0, 0)
	default:
		return "", fmt.Errorf("time_range must be day, week, month or year, got %q", timeRange)
	}
	return start.Format(time.RFC3339), nil
}

func (s *SearchService) filterResults(ctx context.Context, input []exaSearchResult) ([]SearchResultItem, []FilteredSearchResult) {
	results := make([]SearchResultItem, 0, len(input))
	filtered := make(map[string]int)
	for _, item := range input {
		validated, err := s.policy.Validate(ctx, item.URL)
		if err != nil {
			filtered[classifyURLPolicyError(err)]++
			continue
		}
		results = append(results, SearchResultItem{
			Title:         strings.TrimSpace(item.Title),
			URL:           validated.Normalized,
			Snippet:       strings.TrimSpace(strings.Join(item.Highlights, "\n")),
			PublishedDate: strings.TrimSpace(item.PublishedDate),
			Rank:          len(results) + 1,
			Text:          strings.TrimSpace(item.Text),
		})
	}
	return results, filteredSearchResults(filtered)
}

func filteredSearchResults(items map[string]int) []FilteredSearchResult {
	if len(items) == 0 {
		return nil
	}
	out := make([]FilteredSearchResult, 0, len(items))
	for reason, count := range items {
		out = append(out, FilteredSearchResult{Reason: reason, Count: count})
	}
	return out
}

func classifyURLPolicyError(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "private_address"):
		return "private_network"
	case strings.Contains(text, "metadata_address"):
		return "metadata_address"
	case strings.Contains(text, "loopback_address") || strings.Contains(text, "localhost"):
		return "loopback"
	case strings.Contains(text, "link_local_address"):
		return "link_local"
	case strings.Contains(text, "unsupported url scheme"):
		return "unsupported_scheme"
	default:
		return "url_policy_rejected"
	}
}

type exaSearchRequest struct {
	Query              string      `json:"query"`
	NumResults         int         `json:"numResults"`
	StartPublishedDate string      `json:"startPublishedDate,omitempty"`
	Contents           exaContents `json:"contents"`
}

type exaContents struct {
	Highlights bool            `json:"highlights"`
	Text       *exaTextOptions `json:"text,omitempty"`
}

type exaTextOptions struct {
	MaxCharacters int `json:"maxCharacters"`
}

type exaSearchResponse struct {
	Results []exaSearchResult `json:"results"`
}

type exaSearchResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	PublishedDate string   `json:"publishedDate"`
	Highlights    []string `json:"highlights"`
	Text          string   `json:"text"`
}
