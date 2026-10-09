package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// WatchConfig configures the sources the agent follows.
type WatchConfig struct {
	// RSSHubBaseURL is the owner's RSSHub; rsshub:/route targets need it.
	RSSHubBaseURL string `yaml:"rsshub_base_url"`
	// GitHubToken raises the GitHub API rate limit; environment variables expand.
	GitHubToken string `yaml:"github_token"`
	// MaxChecksPerTick bounds how many watches one scheduler tick fetches.
	MaxChecksPerTick int `yaml:"max_checks_per_tick"`
}

// BriefingConfig schedules the morning briefing. At is "HH:MM" in the owner's
// timezone; empty turns the briefing off.
type BriefingConfig struct {
	At string `yaml:"at"`
}

func (c *Config) validateWatch() error {
	if base := strings.TrimSpace(c.Watch.RSSHubBaseURL); base != "" {
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("watch.rsshub_base_url %q must be an http(s) URL", base)
		}
	}
	if c.Watch.MaxChecksPerTick < 1 || c.Watch.MaxChecksPerTick > 50 {
		return errors.New("watch.max_checks_per_tick must be between 1 and 50")
	}
	if at := strings.TrimSpace(c.Briefing.At); at != "" {
		if _, err := ParseClock(at); err != nil {
			return fmt.Errorf("briefing.at: %w", err)
		}
	}
	return nil
}
