package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestMemoryListFactsHandler(t *testing.T) {
	stub := &clientMemoryStub{
		facts: []memorymodule.Record{
			{Ref: "fact_1", Kind: "fact", Title: "Important Fact"},
		},
	}
	server := &Server{memory: stub}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/memory/facts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp MemoryRecordListResponse
	decodeClientTestJSON(t, rec, &resp)
	if len(resp.Items) != 1 || resp.Items[0].Ref != "fact_1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestMemoryListSkillsHandler(t *testing.T) {
	stub := &clientMemoryStub{
		skills: []memorymodule.Record{
			{Ref: "skill_1", Kind: "skill", Title: "Read File"},
		},
	}
	server := &Server{memory: stub}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/memory/skills", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp MemoryRecordListResponse
	decodeClientTestJSON(t, rec, &resp)
	if len(resp.Items) != 1 || resp.Items[0].Ref != "skill_1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestMemoryListHistoryHandler(t *testing.T) {
	stub := &clientMemoryStub{
		history: []memorymodule.Record{
			{Ref: "hist_1", Kind: "history", Title: "Past Event"},
		},
	}
	server := &Server{memory: stub}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/memory/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp MemoryRecordListResponse
	decodeClientTestJSON(t, rec, &resp)
	if len(resp.Items) != 1 || resp.Items[0].Ref != "hist_1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestMemorySearchHandler(t *testing.T) {
	stub := &clientMemoryStub{
		search: []memorymodule.SearchItem{
			{Ref: "fact_1", Kind: "fact", Title: "Found Fact", Score: 0.95},
		},
	}
	server := &Server{memory: stub}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/memory/search?q=test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp MemorySearchResponse
	decodeClientTestJSON(t, rec, &resp)
	if len(resp.Items) != 1 || resp.Items[0].Ref != "fact_1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestMemoryHandlersInvalidLimit(t *testing.T) {
	server := &Server{memory: &clientMemoryStub{}}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/memory/facts?limit=invalid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func newTestRouterForServer(s *Server) http.Handler {
	if s.deviceAuth == nil {
		s.deviceAuth = &deviceAuthHandlerStub{}
	}
	if s.logger == nil {
		s.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	}
	router := chi.NewRouter()
	s.registerRoutes(router)
	return router
}
