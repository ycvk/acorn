package watch

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

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
	feed, err := webaccess.ParseFeed(got.Body)
	if err != nil {
		return Fetched{}, fmt.Errorf("parse feed %s: %w", target, err)
	}
	var items []core.WatchItem
	for _, it := range feed.Items {
		items = append(items, core.WatchItem{
			Key:         it.Key,
			Title:       it.Title,
			URL:         it.URL,
			Summary:     truncate(it.Summary, summaryRunes),
			PublishedAt: it.PublishedAt,
		})
	}
	return Fetched{Items: items}, nil
}
