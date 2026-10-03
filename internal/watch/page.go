package watch

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

func validateSelector(selector string) error {
	if selector == "" {
		return nil
	}
	if _, err := cascadia.ParseGroup(selector); err != nil {
		return fmt.Errorf("selector %q: %w", selector, err)
	}
	return nil
}

// fetchPage takes a snapshot of a page: the text of the elements matching the
// selector, one per line, or the page's main text without a selector.
func (c *Checker) fetchPage(ctx context.Context, w core.Watch) (Fetched, error) {
	var page []byte
	if w.Kind == core.WatchWebRendered {
		rendered, err := c.cfg.Renderer.RenderHTML(ctx, w.Target)
		if err != nil {
			return Fetched{}, fmt.Errorf("render %s: %w", w.Target, err)
		}
		page = []byte(rendered)
	} else {
		got, err := c.cfg.Fetcher.FetchRaw(ctx, w.Target, map[string]string{"Accept": "text/html,application/xhtml+xml"})
		if err != nil {
			return Fetched{}, err
		}
		page = got.Body
	}
	snapshot, err := Snapshot(w.Target, page, w.Selector)
	if err != nil {
		return Fetched{}, err
	}
	return Fetched{Snapshot: snapshot}, nil
}

// Snapshot is the normalized text a web watch compares between checks.
func Snapshot(pageURL string, page []byte, selector string) (string, error) {
	if selector == "" {
		extracted, err := webaccess.ExtractHTML(webaccess.ExtractRequest{URL: pageURL, HTML: page, Mode: webaccess.ExtractionModeReadability})
		if err != nil {
			return "", fmt.Errorf("extract %s: %w", pageURL, err)
		}
		return truncate(strings.TrimSpace(extracted.Markdown), snapshotMax), nil
	}
	sel, err := cascadia.ParseGroup(selector)
	if err != nil {
		return "", fmt.Errorf("selector %q: %w", selector, err)
	}
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", pageURL, err)
	}
	var lines []string
	for _, node := range cascadia.QueryAll(doc, sel) {
		if text := collapse(nodeText(node)); text != "" {
			lines = append(lines, text)
		}
	}
	return truncate(strings.Join(lines, "\n"), snapshotMax), nil
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
		return ""
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(nodeText(child))
		b.WriteString(" ")
	}
	return b.String()
}
