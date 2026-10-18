package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/store"
)

func TestTheOwnerSetsWhatAcornCallsThemFromTheDevice(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	owners, err := NewOwnerService(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		owner:      owners,
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodGet, "/v1/owner", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"name":""}` {
		t.Fatalf("unnamed owner = %d %s", rec.Code, rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodPut, "/v1/owner", `{"name":"  温遥 "}`)
	var saved OwnerDTO
	decodeClientTestJSON(t, rec, &saved)
	if rec.Code != http.StatusOK || saved.Name != "温遥" || saved.UpdatedAt == nil || !saved.UpdatedAt.Equal(now) {
		t.Fatalf("saved = %d %+v", rec.Code, saved)
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/owner", "")
	var current OwnerDTO
	decodeClientTestJSON(t, rec, &current)
	if rec.Code != http.StatusOK || current.Name != "温遥" {
		t.Fatalf("current = %d %+v", rec.Code, current)
	}
	for _, body := range []string{`{"name":"温\n遥"}`, `{"name":"` + strings.Repeat("遥", 41) + `"}`, `{"nickname":"温遥"}`} {
		if rec := performClientRequest(router, http.MethodPut, "/v1/owner", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid save %s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec = performClientRequest(router, http.MethodPut, "/v1/owner", `{"name":""}`)
	decodeClientTestJSON(t, rec, &saved)
	if rec.Code != http.StatusOK || saved.Name != "" {
		t.Fatalf("cleared = %d %+v", rec.Code, saved)
	}
}
