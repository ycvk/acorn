package webaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const jsonFeed = `{"version":"https://jsonfeed.org/version/1.1","title":"Micro","items":[
{"id":"m2","url":"https://m.test/2","content_text":"A post without a title that goes on for a while","date_published":"2026-10-02T12:00:00Z"},
{"id":7,"url":"https://m.test/1","title":"Numbered id","summary":"<p>short</p>","date_published":"2026-10-02T06:00:00Z"}]}`

func TestParseFeedReadsJSONFeeds(t *testing.T) {
	feed, err := ParseFeed([]byte("\ufeff " + jsonFeed))
	if err != nil || feed.Title != "Micro" || len(feed.Items) != 2 {
		t.Fatalf("feed = %+v, %v", feed, err)
	}
	if first := feed.Items[0]; first.Key != "m2" || first.Title != "A post without a title that goes on for a while" || first.PublishedAt.IsZero() {
		t.Fatalf("untitled item = %+v", first)
	}
	if second := feed.Items[1]; second.Key != "7" || second.Title != "Numbered id" || second.Summary != "short" {
		t.Fatalf("numbered item = %+v", second)
	}
	if _, err := ParseFeed([]byte(`{"version":"https://jsonfeed.org/version/1","items":"broken"}`)); err == nil || !strings.Contains(err.Error(), "decode JSON feed") {
		t.Fatalf("a broken JSON feed err = %v", err)
	}
	for _, body := range []string{`{"name":"package.json"}`, `{not json`} {
		if _, err := ParseFeed([]byte(body)); err != ErrNotFeed {
			t.Fatalf("ParseFeed(%s) err = %v, want ErrNotFeed", body, err)
		}
	}
}

func TestFetchListsJSONFeedsAndReturnsOtherJSON(t *testing.T) {
	for contentType, want := range map[string]string{"application/feed+json": "feed", "application/json; charset=utf-8": "feed"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write([]byte(jsonFeed))
		}))
		result, err := newTestFetchService(t, server, 1024*1024).Fetch(context.Background(), FetchRequest{URL: "http://example.com/feed.json"})
		server.Close()
		if err != nil || result.Extracted.Method != want || !strings.Contains(result.Extracted.Markdown, "- [Numbered id](https://m.test/1)") {
			t.Fatalf("%s: %+v, %v", contentType, result.Extracted, err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	result, err := newTestFetchService(t, server, 1024*1024).Fetch(context.Background(), FetchRequest{URL: "http://example.com/api"})
	if err != nil || result.Extracted.Method != "json" || result.Extracted.Markdown != `{"ok":true}` {
		t.Fatalf("other JSON = %+v, %v", result.Extracted, err)
	}
}

func TestDiscoverFeedsTriesLinksAnchorsAndCommonPaths(t *testing.T) {
	served := map[string]string{
		"/blog/": `<html><head><link rel="alternate" type="application/rss+xml; charset=utf-8" href="/blog/posts.rss">
<link rel="alternate" type="text/html" href="/blog/zh/"><link rel="stylesheet" href="/feed.css"></head>
<body><a href="http://example.com/blog/atom.xml#top">Atom</a><a href="/about">About</a><a href="/blog/posts.rss">RSS again</a></body></html>`,
		"/blog/posts.rss": rssFeed,
		"/blog/atom.xml":  atomFeed,
		"/feed.json":      jsonFeed,
		"/rss":            "<html>not a feed</html>",
	}
	var (
		mu    sync.Mutex
		asked []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/feed" {
			http.Redirect(w, r, "/blog/posts.rss", http.StatusMovedPermanently)
			return
		}
		body, ok := served[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	got, err := newTestFetchService(t, server, 1024*1024).DiscoverFeeds(context.Background(), "http://example.com/blog/")
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, f := range got.Feeds {
		listed = append(listed, f.FoundBy+" "+f.URL)
	}
	if strings.Join(listed, "\n") != "link http://example.com/blog/posts.rss\nanchor http://example.com/blog/atom.xml\npath http://example.com/feed.json" {
		t.Fatalf("feeds:\n%s", strings.Join(listed, "\n"))
	}
	if got.Tried != 1+12 {
		t.Fatalf("tried = %d, asked %v", got.Tried, asked)
	}
	// Two entries dated within an hour count as at least 48 a day.
	rss := got.Feeds[0]
	if rss.Title != "Blog" || rss.Entries != 3 || rss.PerDay != 48 || len(rss.Samples) != 3 || rss.Samples[0] != "First & best" {
		t.Fatalf("rss = %+v", rss)
	}
	micro := got.Feeds[2]
	if micro.PerDay != 8 || !micro.Latest.Equal(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("json feed = %+v", micro)
	}
}

func TestDiscoverFeedsTakesAFeedAddressAsIs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(atomFeed))
	}))
	defer server.Close()
	got, err := newTestFetchService(t, server, 1024*1024).DiscoverFeeds(context.Background(), "http://example.com/atom")
	if err != nil || got.Tried != 1 || len(got.Feeds) != 1 || got.Feeds[0].FoundBy != "page" || got.Feeds[0].URL != "http://example.com/atom" {
		t.Fatalf("discovery = %+v, %v", got, err)
	}
}
