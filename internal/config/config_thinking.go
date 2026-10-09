package config

import (
	"errors"
	"fmt"
	"time"
)

// ThinkingConfig schedules reflections in the owner's timezone.
type ThinkingConfig struct {
	NightAt  string   `yaml:"night_at"`
	WanderAt []string `yaml:"wander_at"`
}

func (c *Config) validateThinking() error {
	if c.Wake.DailyTokens < 0 {
		return errors.New("wake.daily_tokens must be >= 0 (0 means unlimited)")
	}
	if c.Thinking.NightAt != "" {
		if _, err := ParseClock(c.Thinking.NightAt); err != nil {
			return fmt.Errorf("thinking.night_at: %w", err)
		}
	}
	if len(c.Thinking.WanderAt) > 6 {
		return errors.New("thinking.wander_at must contain at most 6 times")
	}
	seen := map[time.Duration]bool{}
	for i, at := range c.Thinking.WanderAt {
		parsed, err := ParseClock(at)
		if err != nil {
			return fmt.Errorf("thinking.wander_at[%d]: %w", i, err)
		}
		if seen[parsed] {
			return fmt.Errorf("thinking.wander_at[%d]: duplicate time %q", i, at)
		}
		seen[parsed] = true
	}
	return nil
}
