package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newClientHandlerTestRouter(service *clientHandlerStub) http.Handler {
	server := newClientHotPathServer(service)
	server.deviceAuth = newDeviceAuthTestService(&deviceAuthHandlerStub{})
	server.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	router := chi.NewRouter()
	server.registerRoutes(router)
	return router
}

func performClientRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return performClientRequestWithAuth(handler, method, path, body, "test-token")
}

func performClientRequestWithoutAuth(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return performClientRequestWithAuth(handler, method, path, body, "")
}

func performClientRequestWithAuth(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeClientTestJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, rec.Body.String())
	}
}
