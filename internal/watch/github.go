package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type githubRelease struct {
	ID          int64     `json:"id"`
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
}

type githubIssue struct {
	Number      int64           `json:"number"`
	Title       string          `json:"title"`
	HTMLURL     string          `json:"html_url"`
	Body        string          `json:"body"`
	CreatedAt   time.Time       `json:"created_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}

// fetchGitHub lists a repository's latest releases or newly opened issues.
// Pull requests, which the issues API also returns, are left out.
func (c *Checker) fetchGitHub(ctx context.Context, w core.Watch) (Fetched, error) {
	base := strings.TrimRight(c.cfg.GitHubAPI, "/") + "/repos/" + w.Target
	endpoint := base + "/releases?per_page=20"
	if w.Selector == "issues" {
		endpoint = base + "/issues?state=open&sort=created&direction=desc&per_page=20"
	}
	headers := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	if c.cfg.GitHubToken != "" {
		headers["Authorization"] = "Bearer " + c.cfg.GitHubToken
	}
	got, err := c.cfg.Fetcher.FetchRaw(ctx, endpoint, headers)
	if err != nil {
		return Fetched{}, fmt.Errorf("github %s %s: %w", w.Target, w.Selector, err)
	}
	if w.Selector == "issues" {
		var issues []githubIssue
		if err := json.Unmarshal(got.Body, &issues); err != nil {
			return Fetched{}, fmt.Errorf("github issues of %s: %w", w.Target, err)
		}
		var items []core.WatchItem
		for _, issue := range issues {
			if len(issue.PullRequest) > 0 && string(issue.PullRequest) != "null" {
				continue
			}
			items = append(items, core.WatchItem{
				Key:         "issue-" + strconv.FormatInt(issue.Number, 10),
				Title:       fmt.Sprintf("#%d %s", issue.Number, collapse(issue.Title)),
				URL:         issue.HTMLURL,
				Summary:     truncate(collapse(issue.Body), summaryRunes),
				PublishedAt: issue.CreatedAt,
			})
		}
		return Fetched{Items: items}, nil
	}
	var releases []githubRelease
	if err := json.Unmarshal(got.Body, &releases); err != nil {
		return Fetched{}, fmt.Errorf("github releases of %s: %w", w.Target, err)
	}
	var items []core.WatchItem
	for _, release := range releases {
		if release.Draft {
			continue
		}
		items = append(items, core.WatchItem{
			Key:         "release-" + strconv.FormatInt(release.ID, 10),
			Title:       firstNonEmpty(release.Name, release.TagName),
			URL:         release.HTMLURL,
			Summary:     truncate(collapse(release.Body), summaryRunes),
			PublishedAt: release.PublishedAt,
		})
	}
	return Fetched{Items: items}, nil
}
