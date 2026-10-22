package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
)

// FeedDiscoverer finds the feeds of a page under the outbound URL policy.
type FeedDiscoverer interface {
	DiscoverFeeds(ctx context.Context, page string) (webaccess.FeedDiscovery, error)
}

// FeedDiscoverInput is the input of feed_discover.
type FeedDiscoverInput struct {
	URL string `json:"url" jsonschema:"required" jsonschema_description:"A site, blog or page address, a feed address to check, or rsshub:/route for a route of the owner's RSSHub."`
}

// DiscoveredFeedOutput is one feed found.
type DiscoveredFeedOutput struct {
	URL     string   `json:"url"`
	FoundBy string   `json:"found_by"`
	Title   string   `json:"title,omitempty"`
	Entries int      `json:"entries"`
	PerDay  float64  `json:"per_day"`
	Latest  string   `json:"latest,omitempty"`
	Samples []string `json:"samples"`
}

// FeedDiscoverOutput lists the feeds found and how many addresses were tried.
type FeedDiscoverOutput struct {
	Feeds []DiscoveredFeedOutput `json:"feeds"`
	Tried int                    `json:"tried"`
	Note  string                 `json:"note,omitempty"`
}

func buildFeedDiscoverTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("feed_discover",
		"Find the feeds of a site or page: the address itself when it is a feed, the feeds its <link rel=\"alternate\"> tags name, links on it that look like feeds, and common feed paths, each fetched and checked. found_by says which; per_day is the dated entries over the time they span, at least an hour. Use it before adding a source: prefer ranked feeds such as top stories and digests over a feed of hundreds of entries a day in order of time.",
		func(ctx context.Context, input FeedDiscoverInput, _ ToolProgressEmitter) (FeedDiscoverOutput, error) {
			page := strings.TrimSpace(input.URL)
			if page == "" {
				return FeedDiscoverOutput{}, errors.New("feed_discover: url is required")
			}
			base := strings.TrimRight(deps.RSSHub, "/")
			if strings.HasPrefix(page, "rsshub:") {
				if base == "" {
					return FeedDiscoverOutput{}, errors.New("feed_discover: the owner has no RSSHub: watch.rsshub_base_url is not set")
				}
				page, _ = watch.RSSHubURL(base, page)
			}
			found, err := deps.Feeds.DiscoverFeeds(ctx, page)
			if err != nil {
				return FeedDiscoverOutput{}, fmt.Errorf("feed_discover: %w", err)
			}
			out := FeedDiscoverOutput{Feeds: make([]DiscoveredFeedOutput, 0, len(found.Feeds)), Tried: found.Tried}
			loc, err := ownerLocation(ctx, deps.Settings)
			if err != nil {
				return FeedDiscoverOutput{}, fmt.Errorf("feed_discover: %w", err)
			}
			for _, f := range found.Feeds {
				// A feed on the owner's RSSHub is named by its route, as watches take it.
				url := f.URL
				if route, ok := strings.CutPrefix(url, base+"/"); ok && base != "" {
					url = "rsshub:/" + route
				}
				feed := DiscoveredFeedOutput{
					URL: url, FoundBy: f.FoundBy, Title: f.Title, Entries: f.Entries,
					PerDay: math.Round(f.PerDay*100) / 100, Samples: f.Samples,
				}
				if !f.Latest.IsZero() {
					feed.Latest = f.Latest.In(loc).Format(localTimeLayout)
				}
				out.Feeds = append(out.Feeds, feed)
			}
			if len(out.Feeds) == 0 {
				out.Note = "No feed found. A skill package trigger can read the site and print item lines for a news source of kind trigger."
				if base != "" {
					out.Note = "No feed found. The owner's RSSHub may have a route for the site: check rsshub:/route with feed_discover, then watch_create kind rss with that target. Otherwise a skill package trigger can read the site and print item lines for a news source of kind trigger."
				}
			}
			return out, nil
		})
}
