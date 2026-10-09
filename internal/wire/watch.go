package wire

import (
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/wake"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
)

const defaultGitHubAPI = "https://api.github.com"

// buildWatchChecker builds the checker the scheduler and the watch tools
// share. It renders pages with its own browser when one is configured; the
// returned browser is nil otherwise and must be closed with the container.
func buildWatchChecker(cfg *config.Config, db *store.Store, options buildOptions) (*watch.Checker, *tools.Service, error) {
	webCfg := cfg.WebAccess
	policy := webaccess.URLPolicy{AllowPrivateNetworks: webCfg.AllowPrivateNetworks}
	fetcher, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        webCfg.UserAgent,
		Timeout:          time.Duration(webCfg.TimeoutSeconds) * time.Second,
		MaxResponseBytes: webCfg.MaxResponseBytes,
		Policy:           policy,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("watch fetcher: %w", err)
	}
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
	var browser *tools.Service
	if path := strings.TrimSpace(cfg.Browser.ExecutablePath); path != "" {
		browser, err = tools.NewService(tools.Config{
			ExecutablePath: path,
			Headless:       cfg.Browser.Headless,
			Timeout:        time.Duration(cfg.Browser.DefaultTimeoutSeconds) * time.Second,
			UserAgent:      webCfg.UserAgent,
			Policy:         policy,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("watch browser: %w", err)
		}
		checkerCfg.Renderer = browser
	}
	checker, err := watch.NewChecker(checkerCfg)
	if err != nil {
		return nil, nil, err
	}
	return checker, browser, nil
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
