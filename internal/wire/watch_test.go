package wire

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWatchFetcherReachesTheOwnersRSSHubOnLoopbackOnly(t *testing.T) {
	serve := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
	}
	rsshub := serve("<rss/>")
	defer rsshub.Close()
	other := serve("secret")
	defer other.Close()
	cfg := writeTestConfig(t, "http://provider.test", "watch:\n  rsshub_base_url: "+rsshub.URL+"\n")
	fetcher, _, err := buildWebFetcher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetcher.FetchRaw(context.Background(), rsshub.URL+"/zhihu/hot", nil)
	if err != nil || string(got.Body) != "<rss/>" {
		t.Fatalf("RSSHub fetch = %q, %v", got.Body, err)
	}
	if _, err := fetcher.FetchRaw(context.Background(), other.URL, nil); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("another loopback address err = %v", err)
	}
}
