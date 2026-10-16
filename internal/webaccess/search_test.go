package webaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchServiceSearchesExaAndFiltersURLs(t *testing.T) {
	service := newTestSearchService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "exa-test" {
			t.Fatalf("x-api-key = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["query"] != "acorn" || body["numResults"] != float64(5) {
			t.Fatalf("request = %+v", body)
		}
		if contents, _ := body["contents"].(map[string]any); contents["highlights"] != true || contents["text"] != nil {
			t.Fatalf("contents = %+v", body["contents"])
		}
		if _, ok := body["startPublishedDate"]; ok {
			t.Fatalf("startPublishedDate sent without a time range: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "requestId": "req-1",
  "results": [
    {"title":"Acorn Docs","url":"https://example.com/docs","publishedDate":"2026-05-01T00:00:00.000Z","highlights":["Official docs","Second highlight"]},
    {"title":"Private","url":"http://10.0.0.1/admin","highlights":["private"]}
  ]
}`))
	}))
	result, err := service.Search(context.Background(), SearchRequest{Query: "acorn", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Provider != "exa" || len(result.Results) != 1 {
		t.Fatalf("result = %+v", result)
	}
	got := result.Results[0]
	if got.URL != "https://example.com/docs" || got.Rank != 1 || got.Snippet != "Official docs\nSecond highlight" || got.PublishedDate != "2026-05-01T00:00:00.000Z" {
		t.Fatalf("result[0] = %+v", got)
	}
	if len(result.FilteredResults) != 1 || result.FilteredResults[0].Reason != "private_network" || result.FilteredResults[0].Count != 1 {
		t.Fatalf("filtered = %+v", result.FilteredResults)
	}
	if result.Raw == nil {
		t.Fatal("raw response missing")
	}
}

func TestSearchServiceReturnsTruncatedTextWhenAsked(t *testing.T) {
	service := newTestSearchService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Contents struct {
				Text struct {
					MaxCharacters int `json:"maxCharacters"`
				} `json:"text"`
			} `json:"contents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Contents.Text.MaxCharacters != SearchTextMaxCharacters {
			t.Fatalf("text maxCharacters = %d, want %d", body.Contents.Text.MaxCharacters, SearchTextMaxCharacters)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Acorn Docs","url":"https://example.com/docs","highlights":["Official docs"],"text":"  Acorn runs on your own server.  "}]}`))
	}))
	result, err := service.Search(context.Background(), SearchRequest{Query: "acorn", IncludeText: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Text != "Acorn runs on your own server." {
		t.Fatalf("results = %+v", result.Results)
	}
}

func TestSearchServiceSendsTimeRangeAsPublishedDate(t *testing.T) {
	var start any
	service := newTestSearchService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		start = body["startPublishedDate"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	if _, err := service.Search(context.Background(), SearchRequest{Query: "acorn", TimeRange: "week"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := time.Unix(1_700_000_000, 0).UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	if start != want {
		t.Fatalf("startPublishedDate = %v, want %s", start, want)
	}
}

func TestSearchServiceRejectsUnknownTimeRange(t *testing.T) {
	service := newTestSearchService(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("search sent with an unknown time range")
	}))
	_, err := service.Search(context.Background(), SearchRequest{Query: "acorn", TimeRange: "decade"})
	if err == nil || !strings.Contains(err.Error(), "time_range must be day, week, month or year") {
		t.Fatalf("Search error = %v", err)
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
		APIKey:           "exa-test",
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
