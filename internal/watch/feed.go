package watch

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html/charset"

	"github.com/ycvk/acorn/internal/core"
)

// feedDoc decodes RSS 2.0 (rss>channel>item), RSS 1.0 (RDF>item) and Atom
// (feed>entry) into one shape.
type feedDoc struct {
	ChannelItems []feedItem  `xml:"channel>item"`
	RDFItems     []feedItem  `xml:"item"`
	Entries      []atomEntry `xml:"entry"`
}

type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"date"`
}

type atomEntry struct {
	Title     string     `xml:"title"`
	ID        string     `xml:"id"`
	Links     []atomLink `xml:"link"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

var feedTimeLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST"}

func (c *Checker) fetchFeed(ctx context.Context, w core.Watch) (Fetched, error) {
	target := w.Target
	if route, ok := strings.CutPrefix(target, "rsshub:"); ok {
		target = strings.TrimRight(c.cfg.RSSHubBaseURL, "/") + "/" + strings.TrimLeft(route, "/")
	}
	got, err := c.cfg.Fetcher.FetchRaw(ctx, target, map[string]string{
		"Accept": "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.9",
	})
	if err != nil {
		return Fetched{}, err
	}
	items, err := ParseFeed(got.Body)
	if err != nil {
		return Fetched{}, fmt.Errorf("parse feed %s: %w", target, err)
	}
	return Fetched{Items: items}, nil
}

// ParseFeed reads RSS or Atom entries. An entry is keyed by its guid or id,
// else its link, else its title and date.
func ParseFeed(body []byte) ([]core.WatchItem, error) {
	var doc feedDoc
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel
	decoder.Strict = false
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	var items []core.WatchItem
	for _, it := range append(doc.ChannelItems, doc.RDFItems...) {
		published := firstNonEmpty(it.PubDate, it.Date)
		items = append(items, feedEntry(it.GUID, it.Link, it.Title, it.Description, published))
	}
	for _, e := range doc.Entries {
		items = append(items, feedEntry(e.ID, atomHref(e.Links), e.Title, firstNonEmpty(e.Summary, e.Content), firstNonEmpty(e.Published, e.Updated)))
	}
	if items == nil && !bytes.Contains(body, []byte("<rss")) && !bytes.Contains(body, []byte("<feed")) && !bytes.Contains(body, []byte("RDF")) {
		return nil, fmt.Errorf("not an RSS or Atom feed")
	}
	return items, nil
}

func feedEntry(id, link, title, summary, published string) core.WatchItem {
	id, link, title = strings.TrimSpace(id), strings.TrimSpace(link), collapse(html.UnescapeString(title))
	key := firstNonEmpty(id, link)
	if key == "" {
		key = hashKey(title + "\x00" + published)
	}
	return core.WatchItem{
		Key:         key,
		Title:       firstNonEmpty(title, link, "(untitled)"),
		URL:         link,
		Summary:     truncate(plainText(summary), summaryRunes),
		PublishedAt: parseFeedTime(published),
	}
}

func atomHref(links []atomLink) string {
	for _, l := range links {
		if l.Rel == "" || l.Rel == "alternate" {
			return l.Href
		}
	}
	if len(links) > 0 {
		return links[0].Href
	}
	return ""
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// plainText drops markup from a feed summary.
func plainText(s string) string {
	return collapse(html.UnescapeString(htmlTag.ReplaceAllString(s, " ")))
}

func parseFeedTime(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range feedTimeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
