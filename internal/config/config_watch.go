package config

import (
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
}

func (c *Config) validateWatch() error {
	if base := strings.TrimSpace(c.Watch.RSSHubBaseURL); base != "" {
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("watch.rsshub_base_url %q must be an http(s) URL", base)
		}
	}
	return nil
}
