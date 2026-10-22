package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// UsageReader reads what the daily budgets have used since a time.
type UsageReader interface {
	CountWakesSince(ctx context.Context, since time.Time) (int, error)
	SumAutonomousTokensSince(ctx context.Context, since time.Time) (int, error)
	MemoryUsageSince(ctx context.Context, since time.Time, budget string) (int, error)
}

// Service reads and saves the owner's settings. It reads the store every
// time, so a save from another process applies at the next read.
type Service struct {
	store core.SettingsStore
	usage UsageReader
	clock func() time.Time
}

func NewService(store core.SettingsStore, usage UsageReader, clock func() time.Time) (*Service, error) {
	if store == nil || usage == nil || clock == nil {
		return nil, errors.New("settings service: store, usage and clock are required")
	}
	return &Service{store: store, usage: usage, clock: clock}, nil
}

// Settings returns the settings as they apply now.
func (s *Service) Settings(ctx context.Context) (core.Settings, error) {
	values, _, err := s.values(ctx)
	if err != nil {
		return core.Settings{}, err
	}
	return values.resolve()
}

// Current is the settings as the App and the CLI show them. Customized lists
// the keys the owner saved; the rest are at their defaults.
type Current struct {
	Values     Values
	Defaults   Values
	Customized []string
	Usage      Usage
}

// Usage is what the daily budgets used on Day, the owner-local date.
type Usage struct {
	Day          string `json:"day"`
	Wakes        int    `json:"wakes"`
	WakeTokens   int    `json:"wake_tokens"`
	MemoryTokens int    `json:"memory_tokens"`
	NewsTokens   int    `json:"news_tokens"`
}

func (s *Service) Current(ctx context.Context) (Current, error) {
	values, customized, err := s.values(ctx)
	if err != nil {
		return Current{}, err
	}
	resolved, err := values.resolve()
	if err != nil {
		return Current{}, err
	}
	usage, err := s.usageToday(ctx, resolved.Location)
	if err != nil {
		return Current{}, err
	}
	return Current{Values: values, Defaults: defaults(), Customized: customized, Usage: usage}, nil
}

// Change is one save: settings to set by key, as JSON, and keys to return to
// their defaults.
type Change struct {
	Set   map[string]json.RawMessage
	Reset []string
}

// Save checks every setting in change and saves them together, or none when
// one breaks its rule. Raising the memory or news budget, or lifting its cap,
// puts back in line today's work it deferred to tomorrow.
func (s *Service) Save(ctx context.Context, change Change, deviceID string) (Current, error) {
	if len(change.Set) == 0 && len(change.Reset) == 0 {
		return Current{}, fmt.Errorf("%w: nothing to save", core.ErrSettingInvalid)
	}
	old, _, err := s.values(ctx)
	if err != nil {
		return Current{}, err
	}
	now := s.clock()
	// decode replaces a setting's value without writing into old's slices.
	next := old
	stored := core.SettingsChange{At: now}
	var errs []error
	for _, key := range sortedKeys(change.Set) {
		f, err := fieldFor(key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if slices.Contains(change.Reset, key) {
			errs = append(errs, fmt.Errorf("%w: %s is both set and reset", core.ErrSettingInvalid, key))
			continue
		}
		if err := decode(f, &next, change.Set[key]); err != nil {
			errs = append(errs, err)
			continue
		}
		value, err := json.Marshal(f.ref(&next))
		if err != nil {
			return Current{}, fmt.Errorf("encode %s: %w", key, err)
		}
		stored.Save = append(stored.Save, core.StoredSetting{Key: key, Value: string(value), DeviceID: deviceID, UpdatedAt: now})
	}
	base := defaults()
	for _, key := range change.Reset {
		f, err := fieldFor(key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		raw, err := json.Marshal(f.ref(&base))
		if err != nil {
			return Current{}, fmt.Errorf("encode default %s: %w", key, err)
		}
		if err := decode(f, &next, raw); err != nil {
			return Current{}, err
		}
		stored.Reset = append(stored.Reset, key)
	}
	if err := errors.Join(errs...); err != nil {
		return Current{}, err
	}
	stored.ReleaseMemory = raised(old.MemoryDailyTokens, next.MemoryDailyTokens)
	stored.ReleaseNews = raised(old.NewsDailyTokens, next.NewsDailyTokens)
	if err := s.store.SaveSettings(ctx, stored); err != nil {
		return Current{}, err
	}
	return s.Current(ctx)
}

// Parse turns what the owner typed after a key on the command line into the
// setting's JSON: one value for a text, any number for a list, a start and an
// end or nothing for a window, one whole number for a count.
func Parse(key string, args []string) (json.RawMessage, error) {
	f, err := fieldFor(key)
	if err != nil {
		return nil, err
	}
	var value any
	switch f.kind {
	case kindText:
		if len(args) != 1 {
			return nil, fmt.Errorf("%w: %s takes one value", core.ErrSettingInvalid, key)
		}
		value = args[0]
	case kindList:
		value = append([]string{}, args...)
	case kindWindow:
		switch len(args) {
		case 0:
			value = Window{}
		case 2:
			value = Window{Start: args[0], End: args[1]}
		default:
			return nil, fmt.Errorf("%w: %s takes a start and an end, or nothing to turn it off", core.ErrSettingInvalid, key)
		}
	case kindCount:
		if len(args) != 1 {
			return nil, fmt.Errorf("%w: %s takes one whole number", core.ErrSettingInvalid, key)
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return nil, fmt.Errorf("%w: %s takes one whole number, got %q", core.ErrSettingInvalid, key, args[0])
		}
		value = n
	}
	return json.Marshal(value)
}

// raised reports whether a token budget went up or lost its cap.
func raised(old, next int) bool {
	return old != 0 && (next == 0 || next > old)
}

func (s *Service) values(ctx context.Context) (Values, []string, error) {
	stored, err := s.store.StoredSettings(ctx)
	if err != nil {
		return Values{}, nil, err
	}
	values, err := apply(defaults(), stored)
	if err != nil {
		return Values{}, nil, err
	}
	customized := make([]string, 0, len(stored))
	for _, f := range fields {
		if slices.ContainsFunc(stored, func(s core.StoredSetting) bool { return s.Key == f.key }) {
			customized = append(customized, f.key)
		}
	}
	return values, customized, nil
}

func (s *Service) usageToday(ctx context.Context, loc *time.Location) (Usage, error) {
	local := s.clock().In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	usage := Usage{Day: local.Format(time.DateOnly)}
	var err error
	if usage.Wakes, err = s.usage.CountWakesSince(ctx, midnight); err != nil {
		return Usage{}, err
	}
	if usage.WakeTokens, err = s.usage.SumAutonomousTokensSince(ctx, midnight); err != nil {
		return Usage{}, err
	}
	if usage.MemoryTokens, err = s.usage.MemoryUsageSince(ctx, midnight, "memory"); err != nil {
		return Usage{}, err
	}
	if usage.NewsTokens, err = s.usage.MemoryUsageSince(ctx, midnight, "news"); err != nil {
		return Usage{}, err
	}
	return usage, nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
