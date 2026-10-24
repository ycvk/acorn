package webaccess

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const rssFeed = `<?xml version="1.0"?><rss version="2.0"><channel><title>Blog</title>
<item><title>First &amp; best</title><link>https://blog.test/1</link><guid>g1</guid><description>&lt;p&gt;Hello &lt;b&gt;world&lt;/b&gt;&lt;/p&gt;</description><pubDate>Fri, 02 Oct 2026 08:00:00 +0000</pubDate></item>
<item><title>No guid</title><link>https://blog.test/2</link></item>
<item><title>Untitled</title><pubDate>Fri, 02 Oct 2026 08:00:00 +0000</pubDate></item>
</channel></rss>`

const atomFeed = `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>A</title>
<entry><title>Atom one</title><id>tag:a,1</id><link rel="self" href="https://a.test/self"/><link rel="alternate" href="https://a.test/1"/><summary>sum</summary><updated>2026-10-02T08:00:00Z</updated></entry>
</feed>`

const rdfFeed = `<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<channel><title>Old style</title></channel>
<item><title>RDF one</title><link>https://r.test/1</link><dc:date>2026-10-02T08:00:00Z</dc:date></item>
</rdf:RDF>`

func TestParseFeedReadsRSSAtomAndRDF(t *testing.T) {
	rss, err := ParseFeed([]byte(rssFeed))
	if err != nil || rss.Title != "Blog" || len(rss.Items) != 3 {
		t.Fatalf("rss = %+v, %v", rss, err)
	}
	first := rss.Items[0]
	if first.Key != "g1" || first.Title != "First & best" || first.Summary != "Hello world" || first.PublishedAt.IsZero() {
		t.Fatalf("rss item = %+v", first)
	}
	if rss.Items[1].Key != "https://blog.test/2" {
		t.Fatalf("link key = %q", rss.Items[1].Key)
	}
	// Watches keep these keys; an entry without guid or link hashes its title and date.
	if rss.Items[2].Key != "a5b727fc56400d36550936022ef0c43e" {
		t.Fatalf("hash key = %q", rss.Items[2].Key)
	}

	atom, err := ParseFeed([]byte(atomFeed))
	if err != nil || atom.Title != "A" || len(atom.Items) != 1 || atom.Items[0].Key != "tag:a,1" || atom.Items[0].URL != "https://a.test/1" {
		t.Fatalf("atom = %+v, %v", atom, err)
	}

	rdf, err := ParseFeed([]byte(rdfFeed))
	if err != nil || rdf.Title != "Old style" || len(rdf.Items) != 1 || rdf.Items[0].URL != "https://r.test/1" || rdf.Items[0].PublishedAt.IsZero() {
		t.Fatalf("rdf = %+v, %v", rdf, err)
	}
}

func TestParseFeedKeysAnEntryAcrossItsRevisions(t *testing.T) {
	feed, err := ParseFeed([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>BBC</title>
<item><title>Edited</title><link>https://www.bbc.test/zhongwen/articles/c37/trad?at_medium=RSS</link><guid isPermaLink="false">https://www.bbc.test/zhongwen/articles/c37/trad#3</guid></item>
<item><title>Anchor</title><guid>https://blog.test/notes#top</guid></item>
<item><title>Not a web address</title><guid>tag:blog.test,2026:post#2</guid></item>
<item><title>Link only</title><link>https://blog.test/changelog#12</link></item>
</channel></rss>`))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, item := range feed.Items {
		keys = append(keys, item.Key)
	}
	want := []string{"https://www.bbc.test/zhongwen/articles/c37/trad", "https://blog.test/notes#top", "tag:blog.test,2026:post#2", "https://blog.test/changelog#12"}
	if !slices.Equal(keys, want) {
		t.Fatalf("keys = %q, want %q", keys, want)
	}
}

func TestParseFeedRejectsOtherDocuments(t *testing.T) {
	for _, body := range []string{
		"<html><body>nope</body></html>",
		`<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://a.test/</loc></url></urlset>`,
		"",
	} {
		if _, err := ParseFeed([]byte(body)); !errors.Is(err, ErrNotFeed) {
			t.Fatalf("ParseFeed(%q) error = %v, want ErrNotFeed", body, err)
		}
	}
}

func TestFetchListsAFeedsEntries(t *testing.T) {
	gbkFeed, err := simplifiedchinese.GBK.NewEncoder().String(`<?xml version="1.0" encoding="gbk"?><rss version="2.0"><channel><title>中新网</title>
<item><title>国内要闻一则</title><link>https://n.test/1</link><description>正文摘要</description></item></channel></rss>`)
	if err != nil {
		t.Fatalf("encode GBK feed: %v", err)
	}
	tests := []struct {
		name, contentType, body, title string
		lines                          []string
	}{
		{name: "rss", contentType: "application/rss+xml", body: rssFeed, title: "Blog", lines: []string{
			"# Blog", "3 entries.", "- [First & best](https://blog.test/1) · 2026-10-02 08:00 UTC", "  Hello world", "- [No guid](https://blog.test/2)", "- Untitled · 2026-10-02 08:00 UTC",
		}},
		{name: "atom as application/xml", contentType: "application/xml", body: atomFeed, title: "A", lines: []string{"- [Atom one](https://a.test/1) · 2026-10-02 08:00 UTC", "  sum"}},
		{name: "gbk declared in xml", contentType: "text/xml", body: gbkFeed, title: "中新网", lines: []string{"- [国内要闻一则](https://n.test/1)", "  正文摘要"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := newTestFetchService(t, server, 1024*1024).Fetch(context.Background(), FetchRequest{URL: "http://example.com/feed"})
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if result.Extracted.Method != "feed" || result.Extracted.Title != tc.title || string(result.Raw) != tc.body {
				t.Fatalf("result = method %q, title %q", result.Extracted.Method, result.Extracted.Title)
			}
			lines := strings.Split(result.Extracted.Markdown, "\n")
			for _, want := range tc.lines {
				if !slices.Contains(lines, want) {
					t.Fatalf("markdown lacks line %q:\n%s", want, result.Extracted.Markdown)
				}
			}
		})
	}
}

func TestFetchReturnsOtherXMLAsText(t *testing.T) {
	sitemap := `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://a.test/</loc></url></urlset>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte(sitemap))
	}))
	defer server.Close()
	result, err := newTestFetchService(t, server, 1024*1024).Fetch(context.Background(), FetchRequest{URL: "http://example.com/sitemap.xml"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Extracted.Method != "xml" || result.Extracted.Markdown != sitemap {
		t.Fatalf("result = method %q, markdown %q", result.Extracted.Method, result.Extracted.Markdown)
	}
}

func TestFetchReportsABrokenFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><item><title>cut off`))
	}))
	defer server.Close()
	_, err := newTestFetchService(t, server, 1024*1024).Fetch(context.Background(), FetchRequest{URL: "http://example.com/feed"})
	if err == nil || !strings.Contains(err.Error(), "decode feed") {
		t.Fatalf("Fetch broken feed error = %v", err)
	}
}
