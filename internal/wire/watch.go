package wire

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
)

const defaultGitHubAPI = "https://api.github.com"

// buildWebFetcher fetches under the configured outbound policy, for watches,
// feed discovery and the pages of news issues. The owner's RSSHub is reached
// even on a loopback or private address.
func buildWebFetcher(cfg *config.Config) (*webaccess.FetchService, webaccess.URLPolicy, error) {
	webCfg := cfg.WebAccess
	policy := webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks}
	if base := strings.TrimSpace(cfg.Watch.RSSHubBaseURL); base != "" {
		policy.Trusted = []string{base}
	}
	fetcher, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        webaccess.UserAgent,
		Timeout:          webaccess.FetchTimeout,
		MaxResponseBytes: webaccess.MaxResponseBytes,
		Policy:           policy,
	})
	if err != nil {
		return nil, policy, fmt.Errorf("watch fetcher: %w", err)
	}
	return fetcher, policy, nil
}

// newsPages reads the page of a news issue as Markdown.
type newsPages struct{ fetcher *webaccess.FetchService }

func (p newsPages) ReadPage(ctx context.Context, url string) (string, error) {
	result, err := p.fetcher.Fetch(ctx, webaccess.FetchRequest{URL: url, ExtractMode: webaccess.ExtractionModeAuto})
	if err != nil {
		return "", err
	}
	return result.Extracted.Markdown, nil
}

// buildWatchChecker builds the checker the scheduler and the watch tools
// share. The workstation's browser renders web_rendered watches when a
// workstation is configured.
func buildWatchChecker(cfg *config.Config, db *store.Store, fetcher *webaccess.FetchService, policy webaccess.URLPolicy, options buildOptions) (*watch.Checker, error) {
	githubAPI := defaultGitHubAPI
	if options.githubAPI != "" {
		githubAPI = options.githubAPI
	}
	checkerCfg := watch.Config{
		Store:         db,
		Fetcher:       fetcher,
		Clock:         options.clock,
		RSSHubBaseURL: strings.TrimSpace(cfg.Watch.RSSHubBaseURL),
		GitHubAPI:     githubAPI,
		GitHubToken:   strings.TrimSpace(cfg.Watch.GitHubToken),
	}
	client, err := buildWorkstationClient(cfg)
	if err != nil {
		return nil, err
	}
	if client != nil {
		checkerCfg.Renderer = policyRenderer{policy: policy, renderer: client}
	}
	return watch.NewChecker(checkerCfg)
}
