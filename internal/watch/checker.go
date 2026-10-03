// Package watch follows the owner's sources: it fetches a watch, turns what it
// finds into items, and records the items not seen before. It never calls the
// model; the wake scheduler decides what new items lead to.
package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

const (
	// MinInterval is the shortest time between two checks of a watch.
	MinInterval = 15 * time.Minute
	// FailingAfter consecutive failures mark a watch failing.
	FailingAfter = 5
	maxBackoff   = 24 * time.Hour
	summaryRunes = 300
	snapshotMax  = 4000
)

// Fetcher gets a URL under the outbound URL policy.
type Fetcher interface {
	FetchRaw(ctx context.Context, rawURL string, headers map[string]string) (webaccess.RawResult, error)
}

// Renderer returns a page's HTML after scripts ran.
type Renderer interface {
	RenderHTML(ctx context.Context, rawURL string) (string, error)
}

// Config holds the checker's dependencies. Renderer is optional: without it
// web_rendered watches are unavailable. RSSHubBaseURL is optional: without
// it rsshub:/ targets are unavailable.
type Config struct {
	Store         core.WatchStore
	Fetcher       Fetcher
	Renderer      Renderer
	Clock         func() time.Time
	RSSHubBaseURL string
	GitHubAPI     string
	GitHubToken   string
}

// Fetched is what one fetch of a watch found.
type Fetched struct {
	// Items are feed entries, releases or issues.
	Items []core.WatchItem
	// Snapshot is the selected page content of web watches.
	Snapshot string
}

// Result is a watch after a check and the items it found for the first time.
// Items present at a watch's first check are baseline and not in New.
type Result struct {
	Watch    core.Watch
	New      []core.WatchItem
	Baseline int
}

// Checker fetches watches and records what they found.
type Checker struct {
	cfg Config
}

func NewChecker(cfg Config) (*Checker, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("watch checker: Store is required")
	case cfg.Fetcher == nil:
		return nil, errors.New("watch checker: Fetcher is required")
	case cfg.Clock == nil:
		return nil, errors.New("watch checker: Clock is required")
	case cfg.GitHubAPI == "":
		return nil, errors.New("watch checker: GitHubAPI is required")
	}
	return &Checker{cfg: cfg}, nil
}

var githubRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Validate reports why a watch of kind on target with selector cannot be
// followed, or nil.
func (c *Checker) Validate(kind core.WatchKind, target, selector string) error {
	switch kind {
	case core.WatchRSS:
		if strings.HasPrefix(target, "rsshub:") {
			if c.cfg.RSSHubBaseURL == "" {
				return errors.New("rsshub: targets need watch.rsshub_base_url; deploy RSSHub and set it in the config")
			}
			return nil
		}
		return httpURL(target)
	case core.WatchGitHub:
		if !githubRepo.MatchString(target) {
			return fmt.Errorf("github target %q must be owner/repo", target)
		}
		if selector != "releases" && selector != "issues" {
			return fmt.Errorf("github selector %q must be releases or issues", selector)
		}
		return nil
	case core.WatchWebRendered:
		if c.cfg.Renderer == nil {
			return errors.New("web_rendered watches need a browser; set browser.executable_path in the config")
		}
		fallthrough
	case core.WatchWeb:
		if err := httpURL(target); err != nil {
			return err
		}
		return validateSelector(selector)
	default:
		return fmt.Errorf("unknown watch kind %q; use rss, github, web or web_rendered", kind)
	}
}

// Fetch gets a watch's source without recording anything.
func (c *Checker) Fetch(ctx context.Context, w core.Watch) (Fetched, error) {
	if err := c.Validate(w.Kind, w.Target, w.Selector); err != nil {
		return Fetched{}, err
	}
	switch w.Kind {
	case core.WatchRSS:
		return c.fetchFeed(ctx, w)
	case core.WatchGitHub:
		return c.fetchGitHub(ctx, w)
	default:
		return c.fetchPage(ctx, w)
	}
}

// Check fetches a watch and records the outcome: new items, the next check
// time, and on failure the error with backoff. A fetch failure is returned
// after it is recorded.
func (c *Checker) Check(ctx context.Context, w core.Watch) (Result, error) {
	fetched, err := c.Fetch(ctx, w)
	if err != nil {
		return c.recordFailure(ctx, w, err)
	}
	return c.Apply(ctx, w, fetched)
}

// Apply records a successful fetch of w.
func (c *Checker) Apply(ctx context.Context, w core.Watch, fetched Fetched) (Result, error) {
	now := c.cfg.Clock()
	first := w.LastCheckedAt.IsZero()
	items := fetched.Items
	if w.Kind == core.WatchWeb || w.Kind == core.WatchWebRendered {
		if fetched.Snapshot == "" {
			return c.recordFailure(ctx, w, errors.New("the page or selector produced no text"))
		}
		items = nil
		if !first && fetched.Snapshot != w.Snapshot {
			items = []core.WatchItem{changeItem(w, fetched.Snapshot)}
		}
		w.Snapshot = fetched.Snapshot
	}
	status := core.WatchItemPending
	if first {
		status = core.WatchItemBaseline
	}
	for i := range items {
		items[i].WatchID, items[i].Status, items[i].SeenAt = w.ID, status, now
	}
	added, err := c.cfg.Store.AddWatchItems(ctx, items)
	if err != nil {
		return Result{Watch: w}, err
	}
	w.LastCheckedAt, w.NextCheckAt, w.UpdatedAt = now, now.Add(w.Interval), now
	w.Failures, w.LastError = 0, ""
	if w.Status == core.WatchFailing {
		w.Status = core.WatchActive
	}
	if err := c.cfg.Store.UpdateWatch(ctx, w); err != nil {
		return Result{Watch: w}, err
	}
	if first {
		return Result{Watch: w, Baseline: len(added)}, nil
	}
	return Result{Watch: w, New: added}, nil
}

func (c *Checker) recordFailure(ctx context.Context, w core.Watch, cause error) (Result, error) {
	now := c.cfg.Clock()
	w.Failures++
	w.LastError = cause.Error()
	w.LastCheckedAt, w.UpdatedAt = now, now
	w.NextCheckAt = now.Add(backoff(w.Interval, w.Failures))
	if w.Failures >= FailingAfter && w.Status == core.WatchActive {
		w.Status = core.WatchFailing
	}
	if err := c.cfg.Store.UpdateWatch(ctx, w); err != nil {
		return Result{Watch: w}, errors.Join(cause, fmt.Errorf("record failure: %w", err))
	}
	return Result{Watch: w}, fmt.Errorf("watch #%d %s: %w", w.ID, w.Name, cause)
}

// backoff doubles the interval per consecutive failure, up to a day.
func backoff(interval time.Duration, failures int) time.Duration {
	wait := interval
	for range failures {
		wait *= 2
		if wait >= maxBackoff {
			return maxBackoff
		}
	}
	return wait
}

// changeItem describes a page snapshot that differs from the last one. The key
// covers both snapshots so a value returning to an earlier one is new again.
func changeItem(w core.Watch, snapshot string) core.WatchItem {
	return core.WatchItem{
		Key:     hashKey(w.Snapshot + "\x00" + snapshot),
		Title:   w.Name + " changed",
		URL:     w.Target,
		Summary: "Before: " + truncate(w.Snapshot, 200) + "\nNow: " + truncate(snapshot, 200),
	}
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func httpURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("target %q must be an http(s) URL", target)
	}
	return nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
