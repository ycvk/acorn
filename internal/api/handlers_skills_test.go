package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestListSkillsHandler(t *testing.T) {
	server := &Server{skills: newTestSkillService(t, testSkillFixture{
		id:      "skill.read-file",
		name:    "Read File",
		summary: "Read files safely.",
	})}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/skills", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp SkillListResponse
	decodeClientTestJSON(t, rec, &resp)
	if len(resp.Items) != 1 || resp.Total != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestGetSkillHandler(t *testing.T) {
	server := &Server{skills: newTestSkillService(t, testSkillFixture{
		id:          "skill.read-file",
		name:        "Read File",
		summary:     "Reads files",
		instruction: "Read files from the workspace.",
	})}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/skills/skill.read-file", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp SkillEnvelope
	decodeClientTestJSON(t, rec, &resp)
	if resp.Item.Name != "Read File" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestGetSkillNotFound(t *testing.T) {
	server := &Server{skills: newTestSkillService(t, testSkillFixture{
		id:      "skill.read-file",
		name:    "Read File",
		summary: "Reads files",
	})}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/skills/missing", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func newTestRouterForServer(s *Server) http.Handler {
	if s.deviceAuth == nil {
		s.deviceAuth = newDeviceAuthTestService(&deviceAuthHandlerStub{})
	}
	if s.logger == nil {
		s.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	}
	router := chi.NewRouter()
	s.registerRoutes(router)
	return router
}
