package webaccess

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchServiceFetchesHTMLAndExtractsMarkdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><title>Example Article</title></head>
<body>
  <main>
    <h1>Example Article</h1>
    <p>Acorn can fetch public web pages.</p>
    <a href="/docs">Docs</a>
  </main>
</body>
</html>`))
	}))
	defer server.Close()

	service := newTestFetchService(t, server, 1024*1024)
	result, err := service.Fetch(context.Background(), FetchRequest{
		URL:         "http://example.com/article",
		ExtractMode: ExtractionModeFullPageMarkdown,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.FinalURL != "http://example.com/article" {
		t.Fatalf("final url = %q", result.FinalURL)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d", result.Status)
	}
	if result.Extracted.Method != "full_page_markdown" {
		t.Fatalf("extraction method = %q", result.Extracted.Method)
	}
	if !strings.Contains(result.Extracted.Markdown, "Acorn can fetch public web pages") {
		t.Fatalf("markdown = %q", result.Extracted.Markdown)
	}
	if len(result.Extracted.Links) != 1 || result.Extracted.Links[0].URL != "http://example.com/docs" {
		t.Fatalf("links = %+v", result.Extracted.Links)
	}
	if result.RawSHA256 == "" {
		t.Fatal("raw sha256 is empty")
	}
}

func TestFetchServiceRejectsRedirectToBlockedTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()

	service := newTestFetchService(t, server, 1024*1024)
	_, err := service.Fetch(context.Background(), FetchRequest{URL: "http://example.com/redirect"})
	if err == nil || !strings.Contains(err.Error(), "redirect target rejected") || !strings.Contains(err.Error(), "private_address") {
		t.Fatalf("Fetch redirect error = %v", err)
	}
}

func TestFetchServiceRejectsOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>too large</body></html>"))
	}))
	defer server.Close()

	service := newTestFetchService(t, server, 8)
	_, err := service.Fetch(context.Background(), FetchRequest{URL: "http://example.com/large"})
	if err == nil || !strings.Contains(err.Error(), "max_response_bytes") {
		t.Fatalf("Fetch oversized error = %v", err)
	}
}

func TestFetchServiceRejectsUnsupportedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 2, 3})
	}))
	defer server.Close()

	service := newTestFetchService(t, server, 1024*1024)
	_, err := service.Fetch(context.Background(), FetchRequest{URL: "http://example.com/binary"})
	if err == nil || !strings.Contains(err.Error(), "unsupported web fetch content type") {
		t.Fatalf("Fetch binary error = %v", err)
	}
}

func newTestFetchService(t *testing.T, server *httptest.Server, maxBytes int64) *FetchService {
	t.Helper()
	service, err := NewFetchService(FetchConfig{
		UserAgent:        "Acorn test",
		Timeout:          2 * time.Second,
		MaxResponseBytes: maxBytes,
		Policy: URLPolicy{Resolver: fakeResolver{
			"example.com": {"93.184.216.34"},
		}},
		HTTPClient: httpClientForServer(t, server),
		Now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
	})
	if err != nil {
		t.Fatalf("NewFetchService: %v", err)
	}
	return service
}

func httpClientForServer(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	targetAddr := parsed.Host
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, targetAddr)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport}
}
