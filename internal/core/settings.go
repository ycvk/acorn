package core

import (
	"context"
	"errors"
	"time"
)

// ErrSettingInvalid means a setting's value breaks its rule or names no
// setting; the message names the setting.
var ErrSettingInvalid = errors.New("invalid setting")

// Settings are the owner's schedule, budgets and approval rules as they
// apply now: what the owner saved, and defaults for the rest. Only the owner
// changes them, from the App or the CLI.
type Settings struct {
	// Location is the owner's time zone.
	Location *time.Location
	Briefing DailyTime
	Night    DailyTime
	// Wander holds the idle thought slots as offsets from local midnight.
	Wander     []time.Duration
	QuietHours QuietHours
	// MaxPushesPerHour caps push notifications in any hour.
	MaxPushesPerHour int
	// WakeDailyLimit caps autonomous wakes per owner-local day; zero turns
	// them off.
	WakeDailyLimit int
	// The token budgets are per owner-local day; zero means no cap.
	WakeDailyTokens   int
	MemoryDailyTokens int
	NewsDailyTokens   int
	Approval          ApprovalRules
}

// DailyTime is a time of day as an offset from local midnight. A disabled
// one never comes.
type DailyTime struct {
	Enabled bool
	At      time.Duration
}

// QuietHours is a daily window as offsets from local midnight. Start after
// End means the window crosses midnight; equal values turn it off.
type QuietHours struct {
	Start time.Duration
	End   time.Duration
}

// ApprovalRules are tool-name glob patterns (path.Match syntax). Block
// refuses a call, Require pauses it for the owner's approval, and Allow runs
// it without review; Block wins over Require, and Require over Allow. Calls
// no rule settles go to review.
type ApprovalRules struct {
	Block   []string
	Require []string
	Allow   []string
}

// SettingsReader reads the owner's current settings. Every read sees the
// latest save, including one another process made.
type SettingsReader interface {
	Settings(ctx context.Context) (Settings, error)
}

// StoredSetting is one setting the owner saved, as JSON.
type StoredSetting struct {
	Key   string
	Value string
	// DeviceID is the device the owner saved it from; empty for the CLI.
	DeviceID  string
	UpdatedAt time.Time
}

// SettingsChange is one save. Reset returns settings to their defaults.
// ReleaseMemory and ReleaseNews make the memory jobs and news batches their
// budget deferred to the next day available again at At.
type SettingsChange struct {
	Save          []StoredSetting
	Reset         []string
	ReleaseMemory bool
	ReleaseNews   bool
	At            time.Time
}

// SettingsStore holds the settings the owner saved.
type SettingsStore interface {
	StoredSettings(ctx context.Context) ([]StoredSetting, error)
	SaveSettings(ctx context.Context, change SettingsChange) error
}
