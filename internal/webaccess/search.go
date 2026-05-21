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

const defaultTavilySearchURL = "https://api.tavily.com/search"

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
	Query      string
	MaxResults int
	TimeRange  string
}

type SearchResultItem struct {
	Title         string
	URL           string
	Snippet       string
	Score         float64
	PublishedDate string
	Rank          int
}

type FilteredSearchResult struct {
	Reason string
	Count  int
}

type SearchResult struct {
	Query           string
	Provider        string
	SearchedAt      time.Time
	Results         []SearchResultItem
	FilteredResults []FilteredSearchResult
	Raw             []byte
	ResponseTime    float64
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
		searchURL = defaultTavilySearchURL
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
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	body, err := json.Marshal(tavilySearchRequest{
		Query:             query,
		SearchDepth:       "basic",
		MaxResults:        maxResults,
		TimeRange:         strings.TrimSpace(req.TimeRange),
		IncludeAnswer:     false,
		IncludeRawContent: false,
	})
	if err != nil {
		return SearchResult{}, fmt.Errorf("marshal Tavily search request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.searchURL, bytes.NewReader(body))
	if err != nil {
		return SearchResult{}, fmt.Errorf("create Tavily search request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	client := *s.httpClient
	client.Timeout = s.timeout
	resp, err := client.Do(httpReq)
	if err != nil {
		return SearchResult{}, fmt.Errorf("Tavily search request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body, s.maxResponseBytes)
	if err != nil {
		return SearchResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("Tavily search returned HTTP status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded tavilySearchResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return SearchResult{}, fmt.Errorf("decode Tavily search response: %w", err)
	}
	results, filtered := s.filterResults(ctx, decoded.Results)
	return SearchResult{
		Query:           query,
		Provider:        "tavily",
		SearchedAt:      s.now().UTC(),
		Results:         results,
		FilteredResults: filtered,
		Raw:             raw,
		ResponseTime:    decoded.ResponseTime,
	}, nil
}

func (s *SearchService) filterResults(ctx context.Context, input []tavilySearchResult) ([]SearchResultItem, []FilteredSearchResult) {
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
			Snippet:       strings.TrimSpace(item.Content),
			Score:         item.Score,
			PublishedDate: strings.TrimSpace(item.PublishedDate),
			Rank:          len(results) + 1,
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

type tavilySearchRequest struct {
	Query             string `json:"query"`
	SearchDepth       string `json:"search_depth,omitempty"`
	MaxResults        int    `json:"max_results,omitempty"`
	TimeRange         string `json:"time_range,omitempty"`
	IncludeAnswer     bool   `json:"include_answer"`
	IncludeRawContent bool   `json:"include_raw_content"`
}

type tavilySearchResponse struct {
	Query        string               `json:"query"`
	Results      []tavilySearchResult `json:"results"`
	ResponseTime float64              `json:"response_time"`
}

type tavilySearchResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Score         float64 `json:"score"`
	PublishedDate string  `json:"published_date"`
}
