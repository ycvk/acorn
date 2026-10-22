// Package settings holds the owner's schedule, budgets and approval rules:
// what each setting means, its default and its rule. The owner saves them
// from the App or the CLI; everything that uses one reads its current value.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// The keys follow the config names these settings once had.
const (
	KeyTimezone          = "owner.timezone"
	KeyBriefingAt        = "briefing.at"
	KeyNightAt           = "thinking.night_at"
	KeyWanderAt          = "thinking.wander_at"
	KeyQuietHours        = "notify.quiet_hours"
	KeyMaxPerHour        = "notify.max_per_hour"
	KeyWakeDailyLimit    = "wake.daily_limit"
	KeyWakeDailyTokens   = "wake.daily_tokens"
	KeyMemoryDailyTokens = "memory.daily_tokens"
	KeyNewsDailyTokens   = "news.daily_tokens"
	KeyApprovalBlock     = "approval.block"
	KeyApprovalRequire   = "approval.require"
	KeyApprovalAllow     = "approval.allow"
)

// maxWanderSlots bounds the idle thought slots in a day.
const maxWanderSlots = 6

// Values are the settings as the owner writes them. Each JSON name is the
// setting's key with "_" for ".".
type Values struct {
	Timezone          string   `json:"owner_timezone"`
	BriefingAt        string   `json:"briefing_at"`
	NightAt           string   `json:"thinking_night_at"`
	WanderAt          []string `json:"thinking_wander_at"`
	QuietHours        Window   `json:"notify_quiet_hours"`
	MaxPerHour        int      `json:"notify_max_per_hour"`
	WakeDailyLimit    int      `json:"wake_daily_limit"`
	WakeDailyTokens   int      `json:"wake_daily_tokens"`
	MemoryDailyTokens int      `json:"memory_daily_tokens"`
	NewsDailyTokens   int      `json:"news_daily_tokens"`
	ApprovalBlock     []string `json:"approval_block"`
	ApprovalRequire   []string `json:"approval_require"`
	ApprovalAllow     []string `json:"approval_allow"`
}

// Window is a daily window as "HH:MM" times; both empty turns it off.
type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// kind says how the CLI reads a setting's values.
type kind int

const (
	// kindText takes one value, which may be empty.
	kindText kind = iota
	// kindList takes any number of values.
	kindList
	// kindWindow takes a start and an end, or nothing to turn it off.
	kindWindow
	// kindCount takes one whole number.
	kindCount
)

type field struct {
	key  string
	kind kind
	// ref points at the field of v that holds this setting.
	ref func(v *Values) any
	// tidy checks the setting in v and puts it in its canonical form.
	tidy func(v *Values) error
}

var fields = []field{
	{KeyTimezone, kindText, func(v *Values) any { return &v.Timezone }, func(v *Values) error {
		v.Timezone = strings.TrimSpace(v.Timezone)
		_, err := location(v.Timezone)
		return err
	}},
	{KeyBriefingAt, kindText, func(v *Values) any { return &v.BriefingAt }, func(v *Values) error { return tidyOptionalClock(&v.BriefingAt) }},
	{KeyNightAt, kindText, func(v *Values) any { return &v.NightAt }, func(v *Values) error { return tidyOptionalClock(&v.NightAt) }},
	{KeyWanderAt, kindList, func(v *Values) any { return &v.WanderAt }, func(v *Values) error {
		if len(v.WanderAt) > maxWanderSlots {
			return fmt.Errorf("at most %d times", maxWanderSlots)
		}
		times := make([]string, 0, len(v.WanderAt))
		for _, at := range v.WanderAt {
			offset, err := ParseClock(at)
			if err != nil {
				return err
			}
			text := formatClock(offset)
			if slices.Contains(times, text) {
				return fmt.Errorf("%s appears twice", text)
			}
			times = append(times, text)
		}
		slices.Sort(times)
		v.WanderAt = times
		return nil
	}},
	{KeyQuietHours, kindWindow, func(v *Values) any { return &v.QuietHours }, func(v *Values) error {
		start, end := strings.TrimSpace(v.QuietHours.Start), strings.TrimSpace(v.QuietHours.End)
		if (start == "") != (end == "") {
			return errors.New("needs both start and end, or neither")
		}
		v.QuietHours = Window{}
		if start == "" {
			return nil
		}
		from, err := ParseClock(start)
		if err != nil {
			return err
		}
		to, err := ParseClock(end)
		if err != nil {
			return err
		}
		v.QuietHours = Window{Start: formatClock(from), End: formatClock(to)}
		return nil
	}},
	{KeyMaxPerHour, kindCount, func(v *Values) any { return &v.MaxPerHour }, func(v *Values) error { return atLeast(v.MaxPerHour, 1) }},
	{KeyWakeDailyLimit, kindCount, func(v *Values) any { return &v.WakeDailyLimit }, func(v *Values) error { return atLeast(v.WakeDailyLimit, 0) }},
	{KeyWakeDailyTokens, kindCount, func(v *Values) any { return &v.WakeDailyTokens }, func(v *Values) error { return atLeast(v.WakeDailyTokens, 0) }},
	{KeyMemoryDailyTokens, kindCount, func(v *Values) any { return &v.MemoryDailyTokens }, func(v *Values) error { return atLeast(v.MemoryDailyTokens, 0) }},
	{KeyNewsDailyTokens, kindCount, func(v *Values) any { return &v.NewsDailyTokens }, func(v *Values) error { return atLeast(v.NewsDailyTokens, 0) }},
	{KeyApprovalBlock, kindList, func(v *Values) any { return &v.ApprovalBlock }, func(v *Values) error { return tidyPatterns(&v.ApprovalBlock, true) }},
	{KeyApprovalRequire, kindList, func(v *Values) any { return &v.ApprovalRequire }, func(v *Values) error { return tidyPatterns(&v.ApprovalRequire, true) }},
	{KeyApprovalAllow, kindList, func(v *Values) any { return &v.ApprovalAllow }, func(v *Values) error { return tidyPatterns(&v.ApprovalAllow, false) }},
}

// Keys lists every setting.
func Keys() []string {
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.key
	}
	return keys
}

// KeyForName returns the setting whose JSON name in Values is name.
func KeyForName(name string) (string, error) {
	for _, f := range fields {
		if jsonName(f.key) == name {
			return f.key, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not a setting", core.ErrSettingInvalid, name)
}

// NameForKey returns the JSON name in Values of the setting key.
func NameForKey(key string) string { return jsonName(key) }

func jsonName(key string) string { return strings.ReplaceAll(key, ".", "_") }

func fieldFor(key string) (field, error) {
	for _, f := range fields {
		if f.key == key {
			return f, nil
		}
	}
	return field{}, fmt.Errorf("%w: %q is not a setting", core.ErrSettingInvalid, key)
}

// defaults are the values of settings the owner never saved.
func defaults() Values {
	return Values{
		Timezone:          "UTC",
		BriefingAt:        "08:00",
		NightAt:           "03:00",
		WanderAt:          []string{},
		QuietHours:        Window{Start: "23:00", End: "08:00"},
		MaxPerHour:        6,
		WakeDailyLimit:    20,
		WakeDailyTokens:   0,
		MemoryDailyTokens: 100000,
		NewsDailyTokens:   100000,
		ApprovalBlock:     []string{},
		ApprovalRequire:   []string{"mcp__*", "skill_write", "skill_import", "skill_retire"},
		ApprovalAllow:     []string{},
	}
}

// Defaults are the settings of an owner who never saved one.
func Defaults() core.Settings {
	s, err := defaults().resolve()
	if err != nil {
		panic(fmt.Sprintf("default settings: %v", err))
	}
	return s
}

// Fixed reads s every time.
func Fixed(s core.Settings) core.SettingsReader { return fixed(s) }

type fixed core.Settings

func (f fixed) Settings(context.Context) (core.Settings, error) { return core.Settings(f), nil }

// apply returns v with the stored settings in place of their defaults.
func apply(v Values, stored []core.StoredSetting) (Values, error) {
	for _, s := range stored {
		f, err := fieldFor(s.Key)
		if err != nil {
			return Values{}, fmt.Errorf("stored setting: %w", err)
		}
		if err := decode(f, &v, json.RawMessage(s.Value)); err != nil {
			return Values{}, fmt.Errorf("stored setting %s: %w", s.Key, err)
		}
	}
	return v, nil
}

// decode reads raw into f's place in v, then checks and tidies it.
func decode(f field, v *Values, raw json.RawMessage) error {
	if strings.TrimSpace(string(raw)) == "null" {
		return fmt.Errorf("%w: %s has no value", core.ErrSettingInvalid, f.key)
	}
	target := f.ref(v)
	reflect.ValueOf(target).Elem().SetZero()
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %s: %v", core.ErrSettingInvalid, f.key, err)
	}
	if decoder.More() {
		return fmt.Errorf("%w: %s holds more than one value", core.ErrSettingInvalid, f.key)
	}
	if err := f.tidy(v); err != nil {
		return fmt.Errorf("%w: %s: %v", core.ErrSettingInvalid, f.key, err)
	}
	return nil
}

// resolve turns checked values into the settings everything reads.
func (v Values) resolve() (core.Settings, error) {
	loc, err := location(v.Timezone)
	if err != nil {
		return core.Settings{}, err
	}
	briefing, err := dailyTime(v.BriefingAt)
	if err != nil {
		return core.Settings{}, err
	}
	night, err := dailyTime(v.NightAt)
	if err != nil {
		return core.Settings{}, err
	}
	wander := make([]time.Duration, 0, len(v.WanderAt))
	for _, at := range v.WanderAt {
		offset, err := ParseClock(at)
		if err != nil {
			return core.Settings{}, err
		}
		wander = append(wander, offset)
	}
	var quiet core.QuietHours
	if v.QuietHours.Start != "" {
		if quiet.Start, err = ParseClock(v.QuietHours.Start); err != nil {
			return core.Settings{}, err
		}
		if quiet.End, err = ParseClock(v.QuietHours.End); err != nil {
			return core.Settings{}, err
		}
	}
	return core.Settings{
		Location:          loc,
		Briefing:          briefing,
		Night:             night,
		Wander:            wander,
		QuietHours:        quiet,
		MaxPushesPerHour:  v.MaxPerHour,
		WakeDailyLimit:    v.WakeDailyLimit,
		WakeDailyTokens:   v.WakeDailyTokens,
		MemoryDailyTokens: v.MemoryDailyTokens,
		NewsDailyTokens:   v.NewsDailyTokens,
		Approval: core.ApprovalRules{
			Block:   slices.Clone(v.ApprovalBlock),
			Require: slices.Clone(v.ApprovalRequire),
			Allow:   slices.Clone(v.ApprovalAllow),
		},
	}, nil
}

// location loads an IANA time zone name.
func location(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("%q is not an IANA time zone such as Asia/Shanghai", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%q is not an IANA time zone such as Asia/Shanghai", name)
	}
	return loc, nil
}

// ParseClock parses "HH:MM" into an offset from midnight.
func ParseClock(value string) (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%q must be HH:MM", value)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func formatClock(offset time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(offset/time.Hour), int(offset%time.Hour/time.Minute))
}

func dailyTime(value string) (core.DailyTime, error) {
	if value == "" {
		return core.DailyTime{}, nil
	}
	at, err := ParseClock(value)
	if err != nil {
		return core.DailyTime{}, err
	}
	return core.DailyTime{Enabled: true, At: at}, nil
}

func tidyOptionalClock(value *string) error {
	text := strings.TrimSpace(*value)
	if text == "" {
		*value = ""
		return nil
	}
	offset, err := ParseClock(text)
	if err != nil {
		return err
	}
	*value = formatClock(offset)
	return nil
}

func atLeast(n, least int) error {
	if n < least {
		return fmt.Errorf("must be at least %d", least)
	}
	return nil
}

// tidyPatterns checks tool-name globs. ask_operator already waits on the
// owner: requiring approval for it would ask twice, and blocking it would
// leave the agent unable to ask, so gating rules must not match it.
func tidyPatterns(patterns *[]string, gating bool) error {
	tidied := make([]string, 0, len(*patterns))
	for _, pattern := range *patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return errors.New("a pattern is empty")
		}
		matchesAskOperator, err := path.Match(pattern, "ask_operator")
		if err != nil {
			return fmt.Errorf("%q: %v", pattern, err)
		}
		if gating && matchesAskOperator {
			return fmt.Errorf("%q must not match ask_operator", pattern)
		}
		tidied = append(tidied, pattern)
	}
	*patterns = tidied
	return nil
}
