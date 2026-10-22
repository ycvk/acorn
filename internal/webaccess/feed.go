package webaccess

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

// ErrNotFeed reports an XML document whose root is not an RSS or Atom feed.
var ErrNotFeed = errors.New("not an RSS or Atom feed")

// Feed is an RSS or Atom feed's title and entries, in the feed's order.
type Feed struct {
	Title string
	Items []FeedItem
}

// FeedItem is one feed entry. Key is its guid or id, else its link, else a
// hash of its title and date; Summary is plain text.
type FeedItem struct {
	Key         string
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
}

// feedDoc decodes RSS 2.0 (rss>channel>item), RSS 1.0 (RDF>item) and Atom
// (feed>entry) into one shape.
type feedDoc struct {
	ChannelTitle string      `xml:"channel>title"`
	Title        string      `xml:"title"`
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

// ParseFeed reads an RSS or Atom feed in the encoding its XML declaration
// names. A document with another root element is ErrNotFeed.
func ParseFeed(body []byte) (Feed, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel
	decoder.Strict = false
	root, err := rootElement(decoder)
	if err != nil {
		return Feed{}, err
	}
	switch root.Name.Local {
	case "rss", "feed", "RDF":
	default:
		return Feed{}, ErrNotFeed
	}
	var doc feedDoc
	if err := decoder.DecodeElement(&doc, &root); err != nil {
		return Feed{}, fmt.Errorf("decode feed: %w", err)
	}
	feed := Feed{Title: collapseSpace(html.UnescapeString(firstNonEmpty(doc.ChannelTitle, doc.Title)))}
	for _, it := range append(doc.ChannelItems, doc.RDFItems...) {
		feed.Items = append(feed.Items, feedEntry(it.GUID, it.Link, it.Title, it.Description, firstNonEmpty(it.PubDate, it.Date)))
	}
	for _, e := range doc.Entries {
		feed.Items = append(feed.Items, feedEntry(e.ID, atomHref(e.Links), e.Title, firstNonEmpty(e.Summary, e.Content), firstNonEmpty(e.Published, e.Updated)))
	}
	return feed, nil
}

// rootElement reads up to the document's first element.
func rootElement(decoder *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return xml.StartElement{}, ErrNotFeed
		}
		if err != nil {
			return xml.StartElement{}, fmt.Errorf("decode feed: %w", err)
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start, nil
		}
	}
}

func feedEntry(id, link, title, summary, published string) FeedItem {
	id, link, title = strings.TrimSpace(id), strings.TrimSpace(link), collapseSpace(html.UnescapeString(title))
	key := firstNonEmpty(id, link)
	if key == "" {
		sum := sha256.Sum256([]byte(title + "\x00" + published))
		key = hex.EncodeToString(sum[:16])
	}
	return FeedItem{
		Key:         key,
		Title:       firstNonEmpty(title, link, "(untitled)"),
		URL:         link,
		Summary:     plainText(summary),
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
	return collapseSpace(html.UnescapeString(htmlTag.ReplaceAllString(s, " ")))
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

// feedSummaryRunes caps each entry's summary in a fetched feed's Markdown.
const feedSummaryRunes = 300

// feedMarkdown lists a feed's entries: title with link, date and summary.
func feedMarkdown(feed Feed) string {
	var b strings.Builder
	if feed.Title != "" {
		fmt.Fprintf(&b, "# %s\n\n", feed.Title)
	}
	fmt.Fprintf(&b, "%d entries.\n", len(feed.Items))
	for _, item := range feed.Items {
		b.WriteString("\n- ")
		if item.URL != "" {
			fmt.Fprintf(&b, "[%s](%s)", item.Title, item.URL)
		} else {
			b.WriteString(item.Title)
		}
		if !item.PublishedAt.IsZero() {
			b.WriteString(" · " + item.PublishedAt.Format("2006-01-02 15:04 UTC"))
		}
		if item.Summary != "" {
			b.WriteString("\n  " + truncateRunes(item.Summary, feedSummaryRunes))
		}
	}
	return b.String()
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
