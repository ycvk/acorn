package webaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchServiceSearchesTavilyAndFiltersURLs(t *testing.T) {
	service := newTestSearchService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tvly-test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "query": "acorn",
  "response_time": 0.12,
  "results": [
    {"title":"Acorn Docs","url":"https://example.com/docs","content":"Official docs","score":0.91,"published_date":"2026-05-01"},
    {"title":"Private","url":"http://10.0.0.1/admin","content":"private","score":0.4}
  ]
}`))
	}))
	result, err := service.Search(context.Background(), SearchRequest{Query: "acorn", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("results = %+v", result.Results)
	}
	if result.Results[0].URL != "https://example.com/docs" || result.Results[0].Rank != 1 {
		t.Fatalf("result[0] = %+v", result.Results[0])
	}
	if len(result.FilteredResults) != 1 || result.FilteredResults[0].Reason != "private_network" || result.FilteredResults[0].Count != 1 {
		t.Fatalf("filtered = %+v", result.FilteredResults)
	}
	if result.Raw == nil || result.ResponseTime != 0.12 {
		t.Fatalf("raw/response time = %d/%f", len(result.Raw), result.ResponseTime)
	}
}

func TestSearchServiceRequiresAPIKey(t *testing.T) {
	service, err := NewSearchService(SearchConfig{
		Timeout:          time.Second,
		MaxResults:       10,
		MaxResponseBytes: 1024,
		Policy:           URLPolicy{Resolver: fakeResolver{"example.com": {"93.184.216.34"}}},
		HTTPClient:       &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })},
	})
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	_, err = service.Search(context.Background(), SearchRequest{Query: "acorn"})
	if err == nil || !strings.Contains(err.Error(), "web_access.search.api_key is required") {
		t.Fatalf("Search error = %v", err)
	}
}

func newTestSearchService(t *testing.T, handler http.Handler) *SearchService {
	t.Helper()
	server := httptestServer(t, handler)
	service, err := NewSearchService(SearchConfig{
		APIKey:           "tvly-test",
		Timeout:          time.Second,
		MaxResults:       10,
		MaxResponseBytes: 1024 * 1024,
		Policy: URLPolicy{Resolver: fakeResolver{
			"example.com": {"93.184.216.34"},
		}},
		HTTPClient: server.Client(),
		SearchURL:  server.URL,
		Now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
	})
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	return service
}

func httptestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
