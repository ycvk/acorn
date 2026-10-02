package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OwnerConfig describes the owner. Timezone is an IANA name used to render
// the present and to interpret wake times the owner gives in local time.
type OwnerConfig struct {
	Timezone string `yaml:"timezone"`
}

// PresenceConfig bounds the <presence> block injected before each model call.
type PresenceConfig struct {
	MaxTokens int `yaml:"max_tokens"`
}

// WakeConfig limits autonomous wakes. DailyLimit counts wakes per owner-local
// day; zero disables autonomous wakes.
type WakeConfig struct {
	DailyLimit int `yaml:"daily_limit"`
}

// NotifyConfig configures owner push notifications.
type NotifyConfig struct {
	MaxPerHour int              `yaml:"max_per_hour"`
	QuietHours QuietHoursConfig `yaml:"quiet_hours"`
	FCM        FCMConfig        `yaml:"fcm"`
}

// QuietHoursConfig is a daily window in the owner's timezone, as "HH:MM".
// Notifications inside it are queued until it ends. Equal or empty values
// disable quiet hours.
type QuietHoursConfig struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

// FCMConfig points at a Firebase service account key. Empty disables push.
type FCMConfig struct {
	ServiceAccountFile string `yaml:"service_account_file"`
}

// FCMServiceAccount is the part of a Firebase service account key that FCM
// HTTP v1 needs.
type FCMServiceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// PersonaPath is the owner-editable persona file.
func (c *Config) PersonaPath() string {
	return filepath.Join(c.Runtime.StorageDir, "persona.md")
}

// OwnerLocation returns the owner's timezone.
func (c *Config) OwnerLocation() (*time.Location, error) {
	loc, err := time.LoadLocation(strings.TrimSpace(c.Owner.Timezone))
	if err != nil {
		return nil, fmt.Errorf("owner.timezone %q: %w", c.Owner.Timezone, err)
	}
	return loc, nil
}

// ParseClock parses "HH:MM" into an offset from midnight.
func ParseClock(value string) (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("clock %q must be HH:MM", value)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// LoadFCMServiceAccount reads and checks a Firebase service account key.
func LoadFCMServiceAccount(path string) (FCMServiceAccount, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FCMServiceAccount{}, fmt.Errorf("notify.fcm.service_account_file: %w", err)
	}
	var account FCMServiceAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return FCMServiceAccount{}, fmt.Errorf("notify.fcm.service_account_file %s: %w", path, err)
	}
	for field, value := range map[string]string{
		"project_id": account.ProjectID, "client_email": account.ClientEmail,
		"private_key": account.PrivateKey, "token_uri": account.TokenURI,
	} {
		if strings.TrimSpace(value) == "" {
			return FCMServiceAccount{}, fmt.Errorf("notify.fcm.service_account_file %s: %s is missing", path, field)
		}
	}
	return account, nil
}

// validatePresenceBudget runs after validateContext because the bounds depend
// on the context window.
func (c *Config) validatePresenceBudget() error {
	if c.Presence.MaxTokens < 500 || c.Presence.MaxTokens > c.Context.WindowTokens/4 {
		return fmt.Errorf("presence.max_tokens must be between 500 and context.window_tokens/4 (%d)", c.Context.WindowTokens/4)
	}
	if c.Presence.MaxTokens >= c.Context.CompactMarginTokens {
		return errors.New("presence.max_tokens must be smaller than context.compact_margin_tokens")
	}
	return nil
}

func (c *Config) validatePresence() error {
	if _, err := c.OwnerLocation(); err != nil {
		return err
	}
	if c.Wake.DailyLimit < 0 {
		return errors.New("wake.daily_limit must be >= 0")
	}
	if c.Notify.MaxPerHour < 1 {
		return errors.New("notify.max_per_hour must be >= 1")
	}
	start, end := strings.TrimSpace(c.Notify.QuietHours.Start), strings.TrimSpace(c.Notify.QuietHours.End)
	if (start == "") != (end == "") {
		return errors.New("notify.quiet_hours needs both start and end")
	}
	if start != "" {
		if _, err := ParseClock(start); err != nil {
			return fmt.Errorf("notify.quiet_hours.start: %w", err)
		}
		if _, err := ParseClock(end); err != nil {
			return fmt.Errorf("notify.quiet_hours.end: %w", err)
		}
	}
	if file := c.Notify.FCM.ServiceAccountFile; file != "" {
		if _, err := LoadFCMServiceAccount(file); err != nil {
			return err
		}
	}
	return nil
}
