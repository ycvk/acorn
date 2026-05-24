package web

import (
	"net/http"
	"testing"
)

func TestHealthzHandler(t *testing.T) {
	server := &Server{}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp HealthResponse
	decodeClientTestJSON(t, rec, &resp)
	if !resp.OK {
		t.Fatalf("health check failed: %#v", resp)
	}
}
