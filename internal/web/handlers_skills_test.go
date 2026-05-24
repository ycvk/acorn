package web

import (
	"net/http"
	"testing"

	"github.com/ycvk/acorn/internal/skills"
)

func TestListSkillsHandler(t *testing.T) {
	stub := &clientSkillStub{
		items: []skills.View{
			{Spec: skills.Spec{Name: "Read File"}},
		},
	}
	server := &Server{skills: stub}
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
	stub := &clientSkillStub{
		items: []skills.View{
			{Spec: skills.Spec{Name: "Read File", Description: "Reads files"}},
		},
	}
	server := &Server{skills: stub}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/skills/skill_1", "")
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
	stub := &clientSkillStub{items: []skills.View{}}
	server := &Server{skills: stub}
	router := newTestRouterForServer(server)

	rec := performClientRequest(router, http.MethodGet, "/v1/skills/missing", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
