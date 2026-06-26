package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestLegacyRouteGroupIsNotMounted(t *testing.T) {
	server := newClientHotPathServer(&clientHandlerStub{})
	server.memory = &clientMemoryStub{}
	server.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	router := chi.NewRouter()
	server.registerRoutes(router)

	legacyPrefix := "/" + "api"
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/system/capabilities"},
		{http.MethodGet, "/sessions"},
		{http.MethodPost, "/sessions"},
		{http.MethodGet, "/sessions/last-pending"},
		{http.MethodGet, "/sessions/thread_1"},
		{http.MethodDelete, "/sessions/thread_1"},
		{http.MethodGet, "/sessions/thread_1/messages"},
		{http.MethodPost, "/sessions/thread_1/messages"},
		{http.MethodGet, "/sessions/thread_1/workbench"},
		{http.MethodGet, "/sessions/thread_1/plan"},
		{http.MethodGet, "/runs/run_1/trace"},
		{http.MethodGet, "/runs/run_1/plan"},
		{http.MethodPost, "/runs/run_1/resume"},
		{http.MethodPost, "/runs/run_1/interrupt"},
		{http.MethodGet, "/codeintel/status"},
		{http.MethodGet, "/memory/profile-blocks"},
		{http.MethodGet, "/reflections"},
		{http.MethodGet, "/skills"},
		{http.MethodGet, "/sessions/thread_1/working-checkpoint"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := performClientRequest(router, tc.method, legacyPrefix+tc.path, `{}`)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
