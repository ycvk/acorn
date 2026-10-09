package config

import (
	"errors"
	"fmt"
)

// ThinkingConfig schedules reflections in the owner's timezone.
type ThinkingConfig struct {
	NightAt  string   `yaml:"night_at"`
	WanderAt []string `yaml:"wander_at"`
}

func (c *Config) validateThinking() error {
	if c.Wake.DailyTokens < 1 {
		return errors.New("wake.daily_tokens must be positive")
	}
	if c.Thinking.NightAt != "" {
		if _, err := ParseClock(c.Thinking.NightAt); err != nil {
			return fmt.Errorf("thinking.night_at: %w", err)
		}
	}
	if len(c.Thinking.WanderAt) > 6 {
		return errors.New("thinking.wander_at must contain at most 6 times")
	}
	seen := map[string]bool{}
	for i, at := range c.Thinking.WanderAt {
		if _, err := ParseClock(at); err != nil {
			return fmt.Errorf("thinking.wander_at[%d]: %w", i, err)
		}
		if seen[at] {
			return fmt.Errorf("thinking.wander_at[%d]: duplicate time %q", i, at)
		}
		seen[at] = true
	}
	return nil
}
