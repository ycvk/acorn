package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/store"
)

func TestRenderSettingsMarksDefaultsAndTodaysUse(t *testing.T) {
	values := settingsDefaults(t)
	values.Timezone, values.WanderAt, values.QuietHours = "Asia/Singapore", []string{"10:00", "15:00"}, settings.Window{}
	got, err := renderSettings(settings.Current{
		Values:     values,
		Customized: []string{settings.KeyTimezone, settings.KeyWanderAt, settings.KeyQuietHours},
		Usage:      settings.Usage{Day: "2026-10-22", Wakes: 3, NewsTokens: 1200},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"owner.timezone       Asia/Singapore\n",
		"briefing.at          08:00 (default)\n",
		"thinking.wander_at   10:00, 15:00\n",
		"notify.quiet_hours   off\n",
		"wake.daily_limit     20 (default); 3 used on 2026-10-22\n",
		"news.daily_tokens    100000 (default); 1200 used on 2026-10-22\n",
		"approval.block       none (default)\n",
		"approval.require     mcp__*, skill_write, skill_import, skill_retire (default)\n",
	} {
		if !strings.Contains(got+"\n", want) {
			t.Fatalf("settings lack %q:\n%s", want, got)
		}
	}
}

func TestSettingsSetAndResetSaveForTheNextRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "acorn.yaml")
	body := "runtime:\n  storage_dir: " + filepath.Join(dir, "state") + `
providers:
  - name: primary
    api: chat_completions
    model: fake
    base_url: http://127.0.0.1:1/v1
    api_key: test
    max_output_tokens: 512
    context_window_tokens: 128000
    enabled: true
memory:
  embedding:
    api_key: fixture
    base_url: http://127.0.0.1:1
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, args := range [][]string{
		{"settings", "set", "-c", path, "owner.timezone", "Asia/Singapore"},
		{"settings", "set", "-c", path, "thinking.wander_at", "15:00"},
		{"settings", "set", "-c", path, "news.daily_tokens", "50000"},
		{"settings", "reset", "-c", path, "news.daily_tokens"},
	} {
		if err := Run(ctx, args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if err := Run(ctx, []string{"settings", "set", "-c", path, "briefing.at", "8am"}); err == nil || !strings.Contains(err.Error(), "briefing.at") {
		t.Fatalf("a bad time = %v", err)
	}
	db, err := store.Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := settings.NewService(db, db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.Values.Timezone != "Asia/Singapore" || strings.Join(current.Values.WanderAt, ",") != "15:00" || current.Values.NewsDailyTokens != 100000 || current.Values.BriefingAt != "08:00" {
		t.Fatalf("values = %+v", current.Values)
	}
}

func settingsDefaults(t *testing.T) settings.Values {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := settings.NewService(db, db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return current.Defaults
}
