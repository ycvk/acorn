package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/store"
)

func TestTheOwnerChangesSettingsFromTheDevice(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	owner, err := settings.NewService(db, db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		settings:   owner,
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodGet, "/v1/settings", "")
	var current SettingsDTO
	decodeClientTestJSON(t, rec, &current)
	if rec.Code != http.StatusOK || current.Values.Timezone != "UTC" || current.Values.BriefingAt != "08:00" || len(current.Customized) != 0 || current.Usage.Day != "2026-10-22" {
		t.Fatalf("defaults = %d %+v", rec.Code, current)
	}
	if !strings.Contains(rec.Body.String(), `"thinking_wander_at":[]`) || !strings.Contains(rec.Body.String(), `"approval_require":["mcp__*","skill_write","skill_import","skill_retire"]`) {
		t.Fatalf("defaults body = %s", rec.Body.String())
	}

	rec = performClientRequest(router, http.MethodPatch, "/v1/settings", `{"set":{"owner_timezone":"Asia/Singapore","briefing_at":"9:00","thinking_wander_at":["15:00","10:00"],"news_daily_tokens":50000}}`)
	decodeClientTestJSON(t, rec, &current)
	if rec.Code != http.StatusOK || current.Values.Timezone != "Asia/Singapore" || current.Values.BriefingAt != "09:00" ||
		!slices.Equal(current.Values.WanderAt, []string{"10:00", "15:00"}) || current.Values.NewsDailyTokens != 50000 || current.Defaults.NewsDailyTokens != 100000 {
		t.Fatalf("saved = %d %+v", rec.Code, current)
	}
	if want := []string{"owner_timezone", "briefing_at", "thinking_wander_at", "news_daily_tokens"}; !slices.Equal(current.Customized, want) {
		t.Fatalf("customized = %v, want %v", current.Customized, want)
	}
	stored, err := db.StoredSettings(t.Context())
	if err != nil || len(stored) != 4 || stored[0].DeviceID != "device_test" || !stored[0].UpdatedAt.Equal(now) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	for _, body := range []string{
		`{"set":{"briefing_at":"25:00"}}`,
		`{"set":{"owner_timezone":"Mars/Olympus","notify_max_per_hour":0}}`,
		`{"set":{"approval_require":["ask_*"]}}`,
		`{"set":{"briefing":"08:00"}}`,
		`{"set":{"briefing_at":null}}`,
		`{"reset":["owner.timezone"]}`,
		`{}`,
	} {
		if rec := performClientRequest(router, http.MethodPatch, "/v1/settings", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid save %s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec = performClientRequest(router, http.MethodPatch, "/v1/settings", `{"set":{"briefing_at":"25:00","news_daily_tokens":1}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "briefing.at") {
		t.Fatalf("one bad setting = %d %s", rec.Code, rec.Body.String())
	}
	if values, err := owner.Current(t.Context()); err != nil || values.Values.NewsDailyTokens != 50000 {
		t.Fatalf("a rejected save changed nothing: %+v, %v", values.Values, err)
	}

	rec = performClientRequest(router, http.MethodPatch, "/v1/settings", `{"reset":["briefing_at","news_daily_tokens"]}`)
	decodeClientTestJSON(t, rec, &current)
	if rec.Code != http.StatusOK || current.Values.BriefingAt != "08:00" || current.Values.NewsDailyTokens != 100000 || !slices.Equal(current.Customized, []string{"owner_timezone", "thinking_wander_at"}) {
		t.Fatalf("reset = %d %+v", rec.Code, current)
	}
}
