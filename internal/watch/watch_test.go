package watch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

// fakeFetcher serves fixed bodies by URL and records request headers.
type fakeFetcher struct {
	bodies  map[string]string
	err     error
	headers map[string]string
	urls    []string
}

func (f *fakeFetcher) FetchRaw(_ context.Context, rawURL string, headers map[string]string) (webaccess.RawResult, error) {
	f.urls = append(f.urls, rawURL)
	f.headers = headers
	if f.err != nil {
		return webaccess.RawResult{}, f.err
	}
	body, ok := f.bodies[rawURL]
	if !ok {
		return webaccess.RawResult{}, errors.New("404 " + rawURL)
	}
	return webaccess.RawResult{FinalURL: rawURL, Body: []byte(body)}, nil
}

// itemStore keeps items in memory and the last written watch.
type itemStore struct {
	core.WatchStore
	keys    map[string]bool
	updated core.Watch
}

func (s *itemStore) AddWatchItems(_ context.Context, items []core.WatchItem) ([]core.WatchItem, error) {
	var added []core.WatchItem
	for _, item := range items {
		k := item.Key
		if s.keys[k] {
			continue
		}
		s.keys[k] = true
		item.ID = int64(len(s.keys))
		added = append(added, item)
	}
	return added, nil
}

func (s *itemStore) UpdateWatch(_ context.Context, w core.Watch) error {
	s.updated = w
	return nil
}

var checkNow = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

func newTestChecker(t *testing.T, fetcher *fakeFetcher) (*Checker, *itemStore) {
	t.Helper()
	store := &itemStore{keys: map[string]bool{}}
	c, err := NewChecker(Config{Store: store, Fetcher: fetcher, Clock: func() time.Time { return checkNow }, GitHubAPI: "https://api.github.test", RSSHubBaseURL: "http://rsshub.test", GitHubToken: "ghp_test"})
	if err != nil {
		t.Fatal(err)
	}
	return c, store
}

const rssFeed = `<?xml version="1.0"?><rss version="2.0"><channel><title>Blog</title>
<item><title>First &amp; best</title><link>https://blog.test/1</link><guid>g1</guid><description>&lt;p&gt;Hello &lt;b&gt;world&lt;/b&gt;&lt;/p&gt;</description><pubDate>Fri, 02 Oct 2026 08:00:00 +0000</pubDate></item>
<item><title>No guid</title><link>https://blog.test/2</link></item>
</channel></rss>`

const atomFeed = `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>A</title>
<entry><title>Atom one</title><id>tag:a,1</id><link rel="alternate" href="https://a.test/1"/><summary>sum</summary><updated>2026-10-02T08:00:00Z</updated></entry>
</feed>`

func TestParseFeed(t *testing.T) {
	items, err := ParseFeed([]byte(rssFeed))
	if err != nil || len(items) != 2 {
		t.Fatalf("rss = %+v, %v", items, err)
	}
	if items[0].Key != "g1" || items[0].Title != "First & best" || items[0].Summary != "Hello world" || items[0].PublishedAt.IsZero() {
		t.Fatalf("rss item = %+v", items[0])
	}
	if items[1].Key != "https://blog.test/2" {
		t.Fatalf("link key = %q", items[1].Key)
	}
	atom, err := ParseFeed([]byte(atomFeed))
	if err != nil || len(atom) != 1 || atom[0].Key != "tag:a,1" || atom[0].URL != "https://a.test/1" {
		t.Fatalf("atom = %+v, %v", atom, err)
	}
	if _, err := ParseFeed([]byte("<html><body>nope</body></html>")); err == nil {
		t.Fatal("an HTML page is not a feed")
	}
}

func TestFeedWatchBaselineThenNewItems(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{"http://rsshub.test/github/repos/x": rssFeed}}
	c, store := newTestChecker(t, fetcher)
	w := core.Watch{ID: 1, Name: "blog", Kind: core.WatchRSS, Target: "rsshub:/github/repos/x", Mode: core.WatchModeDigest, Interval: time.Hour, Status: core.WatchActive}
	first, err := c.Check(context.Background(), w)
	if err != nil || first.Baseline != 2 || len(first.New) != 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if !store.updated.NextCheckAt.Equal(checkNow.Add(time.Hour)) || !store.updated.LastCheckedAt.Equal(checkNow) {
		t.Fatalf("watch after first check = %+v", store.updated)
	}
	fetcher.bodies["http://rsshub.test/github/repos/x"] = strings.Replace(rssFeed, "<item><title>No guid", `<item><title>Third</title><guid>g3</guid></item><item><title>No guid`, 1)
	second, err := c.Check(context.Background(), first.Watch)
	if err != nil || len(second.New) != 1 || second.New[0].Key != "g3" || second.New[0].Status != core.WatchItemPending {
		t.Fatalf("second = %+v, %v", second, err)
	}
}

func TestGitHubReleasesAndIssues(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{
		"https://api.github.test/repos/golang/go/releases?per_page=20":                                      `[{"id":7,"tag_name":"go1.27","name":"","html_url":"https://github.com/golang/go/releases/7","body":"notes","published_at":"2026-10-01T00:00:00Z"},{"id":8,"tag_name":"draft","draft":true}]`,
		"https://api.github.test/repos/golang/go/issues?state=open&sort=created&direction=desc&per_page=20": `[{"number":5,"title":"bug","html_url":"u5","created_at":"2026-10-01T00:00:00Z"},{"number":6,"title":"pr","pull_request":{"url":"x"},"created_at":"2026-10-01T00:00:00Z"}]`,
	}}
	c, _ := newTestChecker(t, fetcher)
	releases, err := c.Fetch(context.Background(), core.Watch{Kind: core.WatchGitHub, Target: "golang/go", Selector: "releases"})
	if err != nil || len(releases.Items) != 1 || releases.Items[0].Title != "go1.27" || releases.Items[0].Key != "release-7" {
		t.Fatalf("releases = %+v, %v", releases, err)
	}
	if fetcher.headers["Authorization"] != "Bearer ghp_test" || fetcher.headers["Accept"] != "application/vnd.github+json" {
		t.Fatalf("headers = %v", fetcher.headers)
	}
	issues, err := c.Fetch(context.Background(), core.Watch{Kind: core.WatchGitHub, Target: "golang/go", Selector: "issues"})
	if err != nil || len(issues.Items) != 1 || issues.Items[0].Title != "#5 bug" {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
}

const pricePage = `<html><body><h1>Phone</h1><span class="price"> ¥2,999 </span><script>var x = 1</script></body></html>`

func TestWebWatchReportsSnapshotChanges(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{"https://shop.test/p": pricePage}}
	c, _ := newTestChecker(t, fetcher)
	w := core.Watch{ID: 2, Name: "phone price", Kind: core.WatchWeb, Target: "https://shop.test/p", Selector: ".price", Mode: core.WatchModeImmediate, Interval: time.Hour, Status: core.WatchActive}
	first, err := c.Check(context.Background(), w)
	if err != nil || first.Watch.Snapshot != "¥2,999" || len(first.New) != 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	same, err := c.Check(context.Background(), first.Watch)
	if err != nil || len(same.New) != 0 {
		t.Fatalf("unchanged page produced %+v, %v", same.New, err)
	}
	fetcher.bodies["https://shop.test/p"] = strings.Replace(pricePage, "2,999", "2,799", 1)
	changed, err := c.Check(context.Background(), same.Watch)
	if err != nil || len(changed.New) != 1 || changed.New[0].Summary != "Before: ¥2,999\nNow: ¥2,799" || changed.New[0].Title != "phone price changed" {
		t.Fatalf("changed = %+v, %v", changed.New, err)
	}
	fetcher.bodies["https://shop.test/p"] = pricePage
	back, err := c.Check(context.Background(), changed.Watch)
	if err != nil || len(back.New) != 1 {
		t.Fatalf("returning to an earlier price must be reported: %+v, %v", back.New, err)
	}
}

func TestFailuresBackOffAndMarkFailing(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("connection refused")}
	c, _ := newTestChecker(t, fetcher)
	w := core.Watch{ID: 3, Name: "down", Kind: core.WatchWeb, Target: "https://down.test", Mode: core.WatchModeDigest, Interval: time.Hour, Status: core.WatchActive, LastCheckedAt: checkNow.Add(-time.Hour)}
	for i := 1; i <= FailingAfter; i++ {
		result, err := c.Check(context.Background(), w)
		if err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("check %d err = %v", i, err)
		}
		w = result.Watch
	}
	if w.Status != core.WatchFailing || w.Failures != FailingAfter || w.LastError != "connection refused" || !w.NextCheckAt.Equal(checkNow.Add(maxBackoff)) {
		t.Fatalf("after failures = %+v", w)
	}
	fetcher.err, fetcher.bodies = nil, map[string]string{"https://down.test": pricePage}
	w.Selector = ".price"
	recovered, err := c.Check(context.Background(), w)
	if err != nil || recovered.Watch.Status != core.WatchActive || recovered.Watch.Failures != 0 || recovered.Watch.LastError != "" {
		t.Fatalf("recovered = %+v, %v", recovered.Watch, err)
	}
	empty := w
	empty.Selector = ".missing"
	if _, err := c.Check(context.Background(), empty); err == nil {
		t.Fatal("a selector matching nothing must count as a failure")
	}
}

func TestValidate(t *testing.T) {
	c, _ := newTestChecker(t, &fakeFetcher{})
	noHub, err := NewChecker(Config{Store: &itemStore{}, Fetcher: &fakeFetcher{}, Clock: time.Now, GitHubAPI: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"rsshub without base":      noHub.Validate(core.WatchRSS, "rsshub:/x", ""),
		"rendered without browser": c.Validate(core.WatchWebRendered, "https://x.test", ""),
		"bad repo":                 c.Validate(core.WatchGitHub, "golang", "releases"),
		"bad github selector":      c.Validate(core.WatchGitHub, "golang/go", "stars"),
		"bad selector":             c.Validate(core.WatchWeb, "https://x.test", "[["),
		"not http":                 c.Validate(core.WatchRSS, "ftp://x.test/feed", ""),
		"unknown kind":             c.Validate("email", "x", ""),
	} {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if err := c.Validate(core.WatchWeb, "https://x.test", "div.price, span"); err != nil {
		t.Fatalf("valid web watch: %v", err)
	}
}

func TestRealFetcherKeepsTheURLPolicy(t *testing.T) {
	fetcher, err := webaccess.NewFetchService(webaccess.FetchConfig{UserAgent: "test", Timeout: time.Second, MaxResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := newTestChecker(t, &fakeFetcher{})
	c.cfg.Fetcher = fetcher
	_, err = c.Fetch(context.Background(), core.Watch{Kind: core.WatchRSS, Target: "http://127.0.0.1:9/feed"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("loopback fetch err = %v", err)
	}
}
