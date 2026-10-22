package wire

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/wake"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
)

const defaultGitHubAPI = "https://api.github.com"

// buildWebFetcher fetches under the configured outbound policy, for watches
// and the pages of news issues.
func buildWebFetcher(cfg *config.Config) (*webaccess.FetchService, webaccess.URLPolicy, error) {
	webCfg := cfg.WebAccess
	policy := webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks}
	fetcher, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        webCfg.UserAgent,
		Timeout:          time.Duration(webCfg.TimeoutSeconds) * time.Second,
		MaxResponseBytes: webCfg.MaxResponseBytes,
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

func briefingSchedule(cfg *config.Config) (wake.Briefing, error) {
	at := strings.TrimSpace(cfg.Briefing.At)
	if at == "" {
		return wake.Briefing{}, nil
	}
	offset, err := config.ParseClock(at)
	if err != nil {
		return wake.Briefing{}, fmt.Errorf("briefing.at: %w", err)
	}
	return wake.Briefing{Enabled: true, At: offset}, nil
}

func thinkingSchedule(cfg *config.Config) (wake.Thinking, error) {
	var result wake.Thinking
	if cfg.Thinking.NightAt != "" {
		at, err := config.ParseClock(cfg.Thinking.NightAt)
		if err != nil {
			return result, err
		}
		result.Night = wake.Briefing{Enabled: true, At: at}
	}
	for _, value := range cfg.Thinking.WanderAt {
		at, err := config.ParseClock(value)
		if err != nil {
			return result, err
		}
		result.Wander = append(result.Wander, at)
	}
	return result, nil
}
