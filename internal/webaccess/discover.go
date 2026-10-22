package webaccess

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	// maxFeedCandidates bounds the addresses tried for one page.
	maxFeedCandidates        = 12
	feedDiscoveryConcurrency = 4
	feedSamples              = 3
)

// DiscoveredFeed is a feed found for a page, with what its entries say about
// how often it publishes.
type DiscoveredFeed struct {
	URL string
	// FoundBy is page (the address itself is a feed), link (the page's
	// <link rel="alternate">), anchor (a link on the page) or path (a
	// common feed path).
	FoundBy string
	Title   string
	Entries int
	// PerDay is the dated entries over the time they span, at least an
	// hour; 0 when fewer than two entries are dated.
	PerDay float64
	Latest time.Time
	// Samples are the titles of the first entries.
	Samples []string
}

// FeedDiscovery is the feeds found for a page and how many addresses were
// tried.
type FeedDiscovery struct {
	Feeds []DiscoveredFeed
	Tried int
}

var (
	feedLinkTypes = []string{"application/rss+xml", "application/atom+xml", "application/rdf+xml", "application/feed+json"}
	feedLikePath  = regexp.MustCompile(`(?i)(/(feeds?|rss|atom)/?$|\.(rss|atom)$|(rss|atom|feed)[^/]*\.(xml|json)$)`)
	// pageFeedPaths are tried under the page's directory, rootFeedPaths
	// under the site root.
	pageFeedPaths = []string{"feed", "rss.xml", "atom.xml", "index.xml"}
	rootFeedPaths = []string{"/feed", "/rss", "/rss.xml", "/atom.xml", "/feed.xml", "/index.xml", "/feed.json"}
)

// DiscoverFeeds finds the feeds of a page: the page itself when it is a
// feed, else the feeds its <link rel="alternate"> tags name, links on it that
// look like feeds and common feed paths, each fetched and read under the URL
// policy. Feeds reached at the same final address are listed once.
func (s *FetchService) DiscoverFeeds(ctx context.Context, page string) (FeedDiscovery, error) {
	got, err := s.FetchRaw(ctx, page, map[string]string{
		"Accept": "text/html,application/xhtml+xml,application/rss+xml,application/atom+xml,application/feed+json;q=0.9,*/*;q=0.1",
	})
	if err != nil {
		return FeedDiscovery{}, err
	}
	if feed, err := ParseFeed(got.Body); err == nil {
		return FeedDiscovery{Feeds: []DiscoveredFeed{describeFeed(got.FinalURL, "page", feed)}, Tried: 1}, nil
	} else if !errors.Is(err, ErrNotFeed) {
		return FeedDiscovery{}, err
	}
	base, err := url.Parse(got.FinalURL)
	if err != nil {
		return FeedDiscovery{}, err
	}
	candidates := feedCandidates(base, got.Body)
	found := make([]*DiscoveredFeed, len(candidates))
	var wg sync.WaitGroup
	slots := make(chan struct{}, feedDiscoveryConcurrency)
	for i, c := range candidates {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() {
				<-slots
				wg.Done()
			}()
			raw, err := s.FetchRaw(ctx, c.url, map[string]string{
				"Accept": "application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, text/xml;q=0.9",
			})
			if err != nil {
				return
			}
			if feed, err := ParseFeed(raw.Body); err == nil {
				d := describeFeed(raw.FinalURL, c.foundBy, feed)
				found[i] = &d
			}
		}()
	}
	wg.Wait()
	out := FeedDiscovery{Tried: 1 + len(candidates)}
	seen := map[string]bool{}
	for _, d := range found {
		if d != nil && !seen[d.URL] {
			seen[d.URL] = true
			out.Feeds = append(out.Feeds, *d)
		}
	}
	return out, ctx.Err()
}

type feedCandidate struct{ url, foundBy string }

// feedCandidates lists the addresses to try for a page, in order: its
// <link rel="alternate"> feeds, links that look like feeds, then common
// paths, at most maxFeedCandidates.
func feedCandidates(base *url.URL, body []byte) []feedCandidate {
	var out []feedCandidate
	seen := map[string]bool{}
	add := func(raw, foundBy string) {
		ref, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || raw == "" {
			return
		}
		u := base.ResolveReference(ref)
		u.Fragment = ""
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || seen[u.String()] || len(out) >= maxFeedCandidates {
			return
		}
		seen[u.String()] = true
		out = append(out, feedCandidate{url: u.String(), foundBy: foundBy})
	}
	var links, anchors []string
	if doc, err := html.Parse(bytes.NewReader(body)); err == nil {
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.ElementNode {
				switch strings.ToLower(node.Data) {
				case "link":
					mediaType, _, err := mime.ParseMediaType(attrValue(node, "type"))
					if err == nil && slices.Contains(strings.Fields(strings.ToLower(attrValue(node, "rel"))), "alternate") && slices.Contains(feedLinkTypes, mediaType) {
						links = append(links, attrValue(node, "href"))
					}
				case "a":
					if href := attrValue(node, "href"); href != "" {
						if ref, err := url.Parse(href); err == nil && feedLikePath.MatchString(ref.Path) {
							anchors = append(anchors, href)
						}
					}
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
	}
	for _, href := range links {
		add(href, "link")
	}
	for _, href := range anchors {
		add(href, "anchor")
	}
	if dir := base.Path[:strings.LastIndex(base.Path, "/")+1]; dir != "/" && dir != "" {
		for _, p := range pageFeedPaths {
			add(dir+p, "path")
		}
	}
	for _, p := range rootFeedPaths {
		add(p, "path")
	}
	return out
}

func describeFeed(finalURL, foundBy string, feed Feed) DiscoveredFeed {
	d := DiscoveredFeed{URL: finalURL, FoundBy: foundBy, Title: feed.Title, Entries: len(feed.Items)}
	var oldest time.Time
	dated := 0
	for i, item := range feed.Items {
		if i < feedSamples {
			d.Samples = append(d.Samples, item.Title)
		}
		if item.PublishedAt.IsZero() {
			continue
		}
		dated++
		if item.PublishedAt.After(d.Latest) {
			d.Latest = item.PublishedAt
		}
		if oldest.IsZero() || item.PublishedAt.Before(oldest) {
			oldest = item.PublishedAt
		}
	}
	if dated >= 2 {
		span := max(d.Latest.Sub(oldest), time.Hour)
		d.PerDay = float64(dated) / span.Hours() * 24
	}
	return d
}
