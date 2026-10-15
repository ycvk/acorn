package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/presence"
	"github.com/ycvk/acorn/internal/store"
)

func TestPersonaEditsAreRevisionsFromTheDevice(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if err := presence.SeedPersona(t.Context(), db, now); err != nil {
		t.Fatal(err)
	}
	personas, err := NewPersonaService(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		persona:    personas,
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodGet, "/v1/persona", "")
	var current PersonaDTO
	decodeClientTestJSON(t, rec, &current)
	if rec.Code != http.StatusOK || current.Revision != 1 || current.Author != "release" || !strings.HasPrefix(current.Content, "You are Acorn") {
		t.Fatalf("current = %d %+v", rec.Code, current)
	}

	rec = performClientRequest(router, http.MethodPut, "/v1/persona", `{"content":"  你是 Acorn，说话随意点。\n","base_revision":1}`)
	var saved PersonaDTO
	decodeClientTestJSON(t, rec, &saved)
	if rec.Code != http.StatusOK || saved.Revision != 2 || saved.Content != "你是 Acorn，说话随意点。" || saved.Author != "owner" || saved.DeviceID != "device_test" || !saved.SavedAt.Equal(now) {
		t.Fatalf("saved = %d %+v", rec.Code, saved)
	}
	if rec := performClientRequest(router, http.MethodPut, "/v1/persona", `{"content":"stale","base_revision":1}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "persona_changed") {
		t.Fatalf("stale save = %d %s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{"content":"  ","base_revision":2}`, `{"content":"` + strings.Repeat("x", presence.MaxPersonaBytes+1) + `","base_revision":2}`, `{"content":"x"}`} {
		if rec := performClientRequest(router, http.MethodPut, "/v1/persona", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid save of %d bytes = %d %s", len(body), rec.Code, rec.Body.String())
		}
	}

	rec = performClientRequest(router, http.MethodGet, "/v1/persona/revisions", "")
	var list PersonaRevisionListResponse
	decodeClientTestJSON(t, rec, &list)
	if rec.Code != http.StatusOK || len(list.Revisions) != 2 || list.Revisions[0].Revision != 2 || list.Revisions[1].Author != "release" {
		t.Fatalf("revisions = %d %+v", rec.Code, list)
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/persona/revisions/1", "")
	var first PersonaDTO
	decodeClientTestJSON(t, rec, &first)
	if rec.Code != http.StatusOK || first.Content != current.Content {
		t.Fatalf("revision 1 = %d %+v", rec.Code, first)
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/persona/revisions/9", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing revision = %d", rec.Code)
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/persona/revisions/x", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad revision = %d", rec.Code)
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodGet, "/v1/persona", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", rec.Code)
	}
}
