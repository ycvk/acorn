package config

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ReviewConfig sets up the review of calls the approval rules leave open:
// TypeSafe's Jev judges whether a call has consequences outside the
// workstation, and those pause for the owner's approval. An empty APIKey
// turns review off; environment variables expand.
type ReviewConfig struct {
	APIKey         string `yaml:"api_key"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// ReviewEnabled reports whether calls are reviewed.
func (c *Config) ReviewEnabled() bool {
	key := strings.TrimSpace(c.Review.APIKey)
	return key != "" && !strings.Contains(key, "${")
}

func (c *Config) validateApproval() error {
	for _, rule := range []struct {
		name     string
		patterns []string
	}{{"require", c.Approval.Require}, {"block", c.Approval.Block}, {"allow", c.Approval.Allow}} {
		for i, pattern := range rule.patterns {
			matchesAskOperator, err := path.Match(pattern, "ask_operator")
			if err != nil {
				return fmt.Errorf("approval.%s[%d] %q: %w", rule.name, i, pattern, err)
			}
			// ask_operator already waits on the owner; gating it would ask
			// twice, and blocking it would leave the agent unable to ask.
			if matchesAskOperator && rule.name != "allow" {
				return fmt.Errorf("approval.%s[%d] %q must not match ask_operator", rule.name, i, pattern)
			}
		}
	}
	return nil
}

func (c *Config) validateReview() error {
	if strings.TrimSpace(c.Review.Model) == "" {
		return errors.New("review.model is required")
	}
	if c.Review.TimeoutSeconds < 1 || c.Review.TimeoutSeconds > 120 {
		return errors.New("review.timeout_seconds must be between 1 and 120")
	}
	return nil
}
