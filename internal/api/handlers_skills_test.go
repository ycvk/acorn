package api

import (
	"net/http"
	"testing"
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
