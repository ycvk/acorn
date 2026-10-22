package settings

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type memStore struct {
	saved   map[string]core.StoredSetting
	changes []core.SettingsChange
}

func (m *memStore) StoredSettings(context.Context) ([]core.StoredSetting, error) {
	var out []core.StoredSetting
	for _, s := range m.saved {
		out = append(out, s)
	}
	return out, nil
}

func (m *memStore) SaveSettings(_ context.Context, change core.SettingsChange) error {
	for _, s := range change.Save {
		m.saved[s.Key] = s
	}
	for _, key := range change.Reset {
		delete(m.saved, key)
	}
	m.changes = append(m.changes, change)
	return nil
}

type noUsage struct{}

func (noUsage) CountWakesSince(context.Context, time.Time) (int, error)          { return 3, nil }
func (noUsage) SumAutonomousTokensSince(context.Context, time.Time) (int, error) { return 0, nil }
func (noUsage) MemoryUsageSince(_ context.Context, _ time.Time, budget string) (int, error) {
	return map[string]int{"memory": 1200, "news": 800}[budget], nil
}

func newService(t *testing.T) (*Service, *memStore) {
	t.Helper()
	store := &memStore{saved: map[string]core.StoredSetting{}}
	service, err := NewService(store, noUsage{}, func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func set(pairs ...string) Change {
	change := Change{Set: map[string]json.RawMessage{}}
	for i := 0; i < len(pairs); i += 2 {
		change.Set[pairs[i]] = json.RawMessage(pairs[i+1])
	}
	return change
}

func TestDefaultsMatchWhatTheStarterConfigOnceHad(t *testing.T) {
	d := Defaults()
	if d.Location.String() != "UTC" || d.Briefing != (core.DailyTime{Enabled: true, At: 8 * time.Hour}) || d.Night != (core.DailyTime{Enabled: true, At: 3 * time.Hour}) || len(d.Wander) != 0 {
		t.Fatalf("schedule defaults = %+v", d)
	}
	if d.QuietHours != (core.QuietHours{Start: 23 * time.Hour, End: 8 * time.Hour}) || d.MaxPushesPerHour != 6 {
		t.Fatalf("notify defaults = %+v", d)
	}
	if d.WakeDailyLimit != 20 || d.WakeDailyTokens != 0 || d.MemoryDailyTokens != 100000 || d.NewsDailyTokens != 100000 {
		t.Fatalf("budget defaults = %+v", d)
	}
	if strings.Join(d.Approval.Require, ",") != "mcp__*,skill_write,skill_import,skill_retire" || len(d.Approval.Block) != 0 || len(d.Approval.Allow) != 0 {
		t.Fatalf("approval defaults = %+v", d.Approval)
	}
}

func TestSaveChecksEverySettingAndSavesNoneOnAFailure(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, want string
	}{
		{"zone", KeyTimezone, `"Mars/Olympus"`, "owner.timezone"},
		{"empty zone", KeyTimezone, `""`, "IANA"},
		{"local zone", KeyTimezone, `"Local"`, "IANA"},
		{"briefing", KeyBriefingAt, `"8am"`, "briefing.at"},
		{"night", KeyNightAt, `"25:00"`, "thinking.night_at"},
		{"wander", KeyWanderAt, `["wrong"]`, "thinking.wander_at"},
		{"duplicate", KeyWanderAt, `["12:00","12:00"]`, "appears twice"},
		{"normalized duplicate", KeyWanderAt, `["15:00"," 15:00 "]`, "appears twice"},
		{"many", KeyWanderAt, `["01:00","02:00","03:00","04:00","05:00","06:00","07:00"]`, "at most 6"},
		{"half quiet hours", KeyQuietHours, `{"start":"23:00","end":""}`, "both start and end"},
		{"quiet clock", KeyQuietHours, `{"start":"23:00","end":"8"}`, "notify.quiet_hours"},
		{"quiet field", KeyQuietHours, `{"from":"23:00"}`, "unknown field"},
		{"pushes", KeyMaxPerHour, `0`, "at least 1"},
		{"wake limit", KeyWakeDailyLimit, `-1`, "at least 0"},
		{"tokens", KeyNewsDailyTokens, `1.5`, "news.daily_tokens"},
		{"text count", KeyMemoryDailyTokens, `"100"`, "memory.daily_tokens"},
		{"bad glob", KeyApprovalBlock, `["["]`, "approval.block"},
		{"empty glob", KeyApprovalAllow, `[" "]`, "empty"},
		{"require ask_operator", KeyApprovalRequire, `["shell","ask_*"]`, "must not match ask_operator"},
		{"block ask_operator", KeyApprovalBlock, `["*"]`, "must not match ask_operator"},
		{"null", KeyBriefingAt, `null`, "no value"},
		{"two values", KeyBriefingAt, `"08:00" "09:00"`, "more than one value"},
		{"unknown", "briefing", `"08:00"`, "not a setting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, store := newService(t)
			_, err := service.Save(context.Background(), set(KeyNewsDailyTokens, `50000`, tc.key, tc.value), "device_a")
			if !errors.Is(err, core.ErrSettingInvalid) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Save = %v, want %q", err, tc.want)
			}
			if len(store.changes) != 0 {
				t.Fatalf("a failed save wrote %+v", store.changes)
			}
		})
	}
	service, _ := newService(t)
	if _, err := service.Save(context.Background(), set(KeyApprovalAllow, `["*"]`), ""); err != nil {
		t.Fatalf("allow may match ask_operator: %v", err)
	}
	if _, err := service.Save(context.Background(), Change{}, ""); !errors.Is(err, core.ErrSettingInvalid) {
		t.Fatalf("an empty change = %v", err)
	}
	if _, err := service.Save(context.Background(), Change{Set: set(KeyBriefingAt, `"07:00"`).Set, Reset: []string{KeyBriefingAt}}, ""); err == nil || !strings.Contains(err.Error(), "both set and reset") {
		t.Fatalf("set and reset together = %v", err)
	}
}

func TestSavedValuesAreTidiedAndReadBack(t *testing.T) {
	service, store := newService(t)
	ctx := context.Background()
	current, err := service.Save(ctx, set(
		KeyTimezone, `" Asia/Singapore "`,
		KeyBriefingAt, `"9:00"`,
		KeyNightAt, `""`,
		KeyWanderAt, `["15:00","9:30"]`,
		KeyQuietHours, `{"start":"","end":""}`,
		KeyApprovalBlock, `[" mcp__bank__* "]`,
	), "device_a")
	if err != nil {
		t.Fatal(err)
	}
	v := current.Values
	if v.Timezone != "Asia/Singapore" || v.BriefingAt != "09:00" || v.NightAt != "" || !slices.Equal(v.WanderAt, []string{"09:30", "15:00"}) || v.QuietHours != (Window{}) || !slices.Equal(v.ApprovalBlock, []string{"mcp__bank__*"}) {
		t.Fatalf("values = %+v", v)
	}
	if store.saved[KeyBriefingAt].Value != `"09:00"` || store.saved[KeyBriefingAt].DeviceID != "device_a" {
		t.Fatalf("stored = %+v", store.saved[KeyBriefingAt])
	}
	if want := []string{KeyTimezone, KeyBriefingAt, KeyNightAt, KeyWanderAt, KeyQuietHours, KeyApprovalBlock}; !slices.Equal(current.Customized, want) {
		t.Fatalf("customized = %v", current.Customized)
	}
	if current.Usage != (Usage{Day: "2026-10-23", Wakes: 3, MemoryTokens: 1200, NewsTokens: 800}) {
		t.Fatalf("usage counts the owner-local day: %+v", current.Usage)
	}
	resolved, err := service.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Location.String() != "Asia/Singapore" || resolved.Briefing != (core.DailyTime{Enabled: true, At: 9 * time.Hour}) || resolved.Night.Enabled ||
		!slices.Equal(resolved.Wander, []time.Duration{9*time.Hour + 30*time.Minute, 15 * time.Hour}) || resolved.QuietHours != (core.QuietHours{}) {
		t.Fatalf("settings = %+v", resolved)
	}
	current, err = service.Save(ctx, Change{Reset: []string{KeyBriefingAt, KeyWanderAt}}, "")
	if err != nil || current.Values.BriefingAt != "08:00" || len(current.Values.WanderAt) != 0 || slices.Contains(current.Customized, KeyBriefingAt) {
		t.Fatalf("reset = %+v, %v", current, err)
	}
}

func TestRaisingABudgetReleasesWhatItDeferred(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after string
		releaseMemory bool
	}{
		{"raised", `1000`, `2000`, true},
		{"lifted", `1000`, `0`, true},
		{"lowered", `1000`, `500`, false},
		{"from no cap", `0`, `1000`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, store := newService(t)
			ctx := context.Background()
			if _, err := service.Save(ctx, set(KeyMemoryDailyTokens, tc.before), ""); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Save(ctx, set(KeyMemoryDailyTokens, tc.after), ""); err != nil {
				t.Fatal(err)
			}
			last := store.changes[len(store.changes)-1]
			if last.ReleaseMemory != tc.releaseMemory || last.ReleaseNews {
				t.Fatalf("change = %+v", last)
			}
		})
	}
	service, store := newService(t)
	if _, err := service.Save(context.Background(), set(KeyNewsDailyTokens, `200000`), ""); err != nil {
		t.Fatal(err)
	}
	if last := store.changes[0]; !last.ReleaseNews || last.ReleaseMemory {
		t.Fatalf("raising the news budget from its default = %+v", last)
	}
}

func TestParseReadsCommandLineValues(t *testing.T) {
	for _, tc := range []struct {
		key  string
		args []string
		want string
	}{
		{KeyTimezone, []string{"Asia/Singapore"}, `"Asia/Singapore"`},
		{KeyBriefingAt, []string{""}, `""`},
		{KeyWanderAt, nil, `[]`},
		{KeyWanderAt, []string{"10:00", "15:00"}, `["10:00","15:00"]`},
		{KeyQuietHours, []string{"23:00", "08:00"}, `{"start":"23:00","end":"08:00"}`},
		{KeyQuietHours, nil, `{"start":"","end":""}`},
		{KeyNewsDailyTokens, []string{"50000"}, `50000`},
	} {
		got, err := Parse(tc.key, tc.args)
		if err != nil || string(got) != tc.want {
			t.Fatalf("Parse(%s, %q) = %s, %v; want %s", tc.key, tc.args, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		key  string
		args []string
	}{
		{KeyTimezone, nil},
		{KeyQuietHours, []string{"23:00"}},
		{KeyNewsDailyTokens, []string{"lots"}},
		{"owner.name", []string{"x"}},
	} {
		if _, err := Parse(tc.key, tc.args); !errors.Is(err, core.ErrSettingInvalid) {
			t.Fatalf("Parse(%s, %q) = %v", tc.key, tc.args, err)
		}
	}
}

func TestNamesAreKeysWithUnderscores(t *testing.T) {
	for _, key := range Keys() {
		name := NameForKey(key)
		if got, err := KeyForName(name); err != nil || got != key {
			t.Fatalf("KeyForName(%s) = %s, %v", name, got, err)
		}
	}
	body, err := json.Marshal(defaults())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != len(Keys()) {
		t.Fatalf("Values has %d JSON fields for %d settings", len(fields), len(Keys()))
	}
	for _, key := range Keys() {
		if _, ok := fields[NameForKey(key)]; !ok {
			t.Fatalf("Values lacks %s", NameForKey(key))
		}
	}
}
