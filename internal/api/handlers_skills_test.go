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

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

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

func TestGetSkillNotFound(t *testing.T) {
	router := newTestRouterForServer(&Server{skills: newTestSkillService(t, testSkillFixture{name: "read-file", description: "Reads files"})})
	rec := performClientRequest(router, http.MethodGet, "/v1/skills/missing", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "skill_not_found") {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
}

// TestSkillListShowsEverySkillTheOwnerHas lists retired skills and a skill
// stored with a manifest that no longer parses, which shows as a problem
// without failing the list or its page.
func TestSkillListShowsEverySkillTheOwnerHas(t *testing.T) {
	env := newSkillTestEnv(t)
	env.importSkill(t, core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("notes", "Write notes.", "Write them."))})
	at := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	if _, err := env.db.WriteSkill(t.Context(), core.SkillWrite{
		ID:       "fetch",
		Markdown: skillMarkdown("fetch", "Fetch pages.", "Use the fetch tools."),
		Files:    []core.SkillFile{{Path: skills.ManifestFileName, Content: []byte("mcp:\n  command: uvx\n  args: [mcp-server-fetch]\n  secrets: [FETCH_TOKEN]\n")}},
		Retired:  true,
		Author:   core.SkillAuthorAgent,
		RunID:    "run-fetch",
		At:       at,
	}); err != nil {
		t.Fatal(err)
	}
	retired := env.importSkill(t, core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("old-way", "An old way.", "Do it the old way."))})
	if _, err := env.library.RetireForOwner(t.Context(), "old-way", retired.Revision); err != nil {
		t.Fatal(err)
	}
	router := newTestRouterForServer(&Server{skills: env.service})

	rec := performClientRequest(router, http.MethodGet, "/v1/skills", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body %s", rec.Code, rec.Body.String())
	}
	var list SkillListResponse
	decodeClientTestJSON(t, rec, &list)
	byName := map[string]SkillSummaryDTO{}
	for _, item := range list.Items {
		byName[item.Name] = item
	}
	fetch, notes, old := byName["fetch"], byName["notes"], byName["old-way"]
	if len(list.Items) != 3 || !fetch.Retired || !fetch.HasManifest || fetch.Eligible || !strings.Contains(fetch.Problem, "field secrets not found") || fetch.Author != "agent" || !fetch.UpdatedAt.Equal(at) {
		t.Fatalf("fetch = %+v", fetch)
	}
	if !notes.Eligible || notes.Retired || notes.Author != "owner" || notes.Revision != 1 || notes.Problem != "" || notes.Description != "Write notes." {
		t.Fatalf("notes = %+v", notes)
	}
	if !old.Retired || old.Eligible || old.Revision != 2 || old.Author != "owner" {
		t.Fatalf("old-way = %+v", old)
	}

	rec = performClientRequest(router, http.MethodGet, "/v1/skills/fetch", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body %s", rec.Code, rec.Body.String())
	}
	var page SkillEnvelope
	decodeClientTestJSON(t, rec, &page)
	if page.Item.Problem == "" || page.Item.Package != nil || page.Item.RunID != "run-fetch" || len(page.Item.Files) != 1 || page.Item.Files[0].Content == nil {
		t.Fatalf("fetch page = %+v", page.Item)
	}
	rec = performClientRequest(router, http.MethodPost, "/v1/skills/fetch:restore", `{"base_revision":1}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "skill_change_invalid") {
		t.Fatalf("restoring a package whose manifest does not parse = %d body %s", rec.Code, rec.Body.String())
	}
}

// TestSkillRollbackIsTheOwnersRevision rolls back the agent's change as a
// new owner revision based on the revision the owner read; a stale base is
// a conflict that writes nothing.
func TestSkillRollbackIsTheOwnersRevision(t *testing.T) {
	env := newSkillTestEnv(t)
	env.importSkill(t,
		core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("orders", "Check orders.", "Open the orders page.\nRead the latest order."))},
		core.SkillFile{Path: "references/sites.md", Content: []byte("shop.example\n")},
	)
	if _, err := env.library.Write(t.Context(), skills.WriteRequest{
		Markdown:     skillMarkdown("orders", "Check orders.", "Open the orders page.\nRead every order."),
		Put:          map[string]string{"scripts/fetch.sh": "#!/bin/sh\necho orders\n"},
		Delete:       []string{"references/sites.md"},
		BaseRevision: 1,
		RunID:        "run-agent",
	}); err != nil {
		t.Fatal(err)
	}
	router := newTestRouterForServer(&Server{skills: env.service})

	rec := performClientRequest(router, http.MethodGet, "/v1/skills/orders/revisions", "")
	var history SkillRevisionListResponse
	decodeClientTestJSON(t, rec, &history)
	if rec.Code != http.StatusOK || len(history.Revisions) != 2 || history.Revisions[0].Revision != 2 || history.Revisions[0].Author != "agent" || history.Revisions[0].RunID != "run-agent" || history.Revisions[1].Author != "owner" {
		t.Fatalf("history = %d %+v", rec.Code, history)
	}

	rec = performClientRequest(router, http.MethodPost, "/v1/skills/orders:rollback", `{"revision":1,"base_revision":1}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "skill_changed") {
		t.Fatalf("rollback on a stale base = %d body %s", rec.Code, rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodPost, "/v1/skills/orders:rollback", `{"revision":1,"base_revision":2}`)
	var rolled SkillSummaryDTO
	decodeClientTestJSON(t, rec, &rolled)
	if rec.Code != http.StatusOK || rolled.Revision != 3 || rolled.Author != "owner" || rolled.Retired || !rolled.Eligible {
		t.Fatalf("rollback = %d %+v", rec.Code, rolled)
	}

	rec = performClientRequest(router, http.MethodGet, "/v1/skills/orders", "")
	var page SkillEnvelope
	decodeClientTestJSON(t, rec, &page)
	if page.Item.Revision != 3 || page.Item.RunID != "" || !strings.Contains(page.Item.Markdown, "Read the latest order.") || len(page.Item.Files) != 1 || page.Item.Files[0].Path != "references/sites.md" {
		t.Fatalf("page after rollback = %+v", page.Item)
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/skills/orders/revisions", "")
	decodeClientTestJSON(t, rec, &history)
	if len(history.Revisions) != 3 || history.Revisions[0].SHA256 != history.Revisions[2].SHA256 || history.Revisions[0].SHA256 == history.Revisions[1].SHA256 || history.Revisions[0].SHA256 != page.Item.SHA256 {
		t.Fatalf("history after rollback = %+v", history)
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/skills/orders/revisions/2", "")
	var second SkillRevisionDTO
	decodeClientTestJSON(t, rec, &second)
	if rec.Code != http.StatusOK || second.Revision != 2 || !strings.Contains(second.Markdown, "Read every order.") || second.Instruction == "" || len(second.Files) != 1 || second.Files[0].Path != "scripts/fetch.sh" {
		t.Fatalf("revision 2 = %d %+v", rec.Code, second)
	}
	for _, path := range []string{"/v1/skills/orders/revisions/9", "/v1/skills/orders/diff?from=1&to=9"} {
		if rec := performClientRequest(router, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d body %s", path, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"/v1/skills/orders/revisions/0", "/v1/skills/orders/diff?from=1", "/v1/skills/orders/diff?from=x&to=2"} {
		if rec := performClientRequest(router, http.MethodGet, path, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("GET %s = %d body %s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestSkillDiffComesFromTheServer diffs two revisions file by file: SKILL.md
// by lines with context, then the other files by path.
func TestSkillDiffComesFromTheServer(t *testing.T) {
	env := newSkillTestEnv(t)
	body := "Step 1.\nStep 2.\nStep 3.\nStep 4.\nStep 5.\nStep 6.\nStep 7.\nStep 8.\nStep 9.\nStep 10."
	env.importSkill(t,
		core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("steps", "Follow steps.", body))},
		core.SkillFile{Path: "references/a.md", Content: []byte("keep\n")},
		core.SkillFile{Path: "references/b.md", Content: []byte("gone\n")},
		core.SkillFile{Path: "assets/logo.png", Content: []byte{0x89, 'P', 'N', 'G', 0, 1}},
	)
	if _, err := env.library.Write(t.Context(), skills.WriteRequest{
		Markdown:     skillMarkdown("steps", "Follow steps.", strings.Replace(body, "Step 2.", "Step two.", 1)),
		Put:          map[string]string{"references/c.md": "new\nfile\n", "assets/logo.png": "\x89PNG\x00\x02"},
		Delete:       []string{"references/b.md"},
		BaseRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	router := newTestRouterForServer(&Server{skills: env.service})

	rec := performClientRequest(router, http.MethodGet, "/v1/skills/steps/diff?from=1&to=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("diff status = %d body %s", rec.Code, rec.Body.String())
	}
	var diff SkillDiffDTO
	decodeClientTestJSON(t, rec, &diff)
	var paths []string
	for _, f := range diff.Files {
		paths = append(paths, f.Path+":"+f.Change)
	}
	if !slices.Equal(paths, []string{"SKILL.md:modified", "assets/logo.png:modified", "references/b.md:removed", "references/c.md:added"}) || diff.From != 1 || diff.To != 2 {
		t.Fatalf("diff files = %v", paths)
	}
	markdown := diff.Files[0]
	if len(markdown.Hunks) != 1 || markdown.Binary {
		t.Fatalf("SKILL.md hunks = %+v", markdown)
	}
	var lines []string
	for _, l := range markdown.Hunks[0].Lines {
		lines = append(lines, l.Kind+" "+l.Text)
	}
	if !slices.Equal(lines, []string{"same ---", "same ", "same Step 1.", "removed Step 2.", "added Step two.", "same Step 3.", "same Step 4.", "same Step 5."}) || markdown.Hunks[0].OldStart != 4 || markdown.Hunks[0].NewStart != 4 {
		t.Fatalf("SKILL.md hunk = %+v: %q", markdown.Hunks[0], lines)
	}
	if logo := diff.Files[1]; !logo.Binary || len(logo.Hunks) != 0 {
		t.Fatalf("binary diff = %+v", logo)
	}
	if added := diff.Files[3]; len(added.Hunks) != 1 || len(added.Hunks[0].Lines) != 2 || added.Hunks[0].Lines[0].Kind != "added" || added.Hunks[0].OldStart != 1 {
		t.Fatalf("added file diff = %+v", added)
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/skills/steps/diff?from=2&to=2", "")
	decodeClientTestJSON(t, rec, &diff)
	if len(diff.Files) != 0 {
		t.Fatalf("a revision against itself = %+v", diff.Files)
	}
}

// TestSkillRetireAndRestoreCheckTheRevision retires and restores a skill as
// the owner's revisions, each based on the revision the owner read.
func TestSkillRetireAndRestoreCheckTheRevision(t *testing.T) {
	env := newSkillTestEnv(t)
	env.importSkill(t, core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("orders", "Check orders.", "Open the orders page."))})
	router := newTestRouterForServer(&Server{skills: env.service})

	for _, tc := range []struct {
		path, body string
		status     int
		want       string
	}{
		{"/v1/skills/orders:retire", `{"base_revision":2}`, http.StatusConflict, "skill_changed"},
		{"/v1/skills/orders:restore", `{"base_revision":1}`, http.StatusBadRequest, "skill_change_invalid"},
		{"/v1/skills/orders:retire", `{"base_revision":0}`, http.StatusBadRequest, "base_revision"},
		{"/v1/skills/orders:retire", `{"base_revision":1}`, http.StatusOK, `"retired":true`},
		{"/v1/skills/orders:retire", `{"base_revision":2}`, http.StatusBadRequest, "already retired"},
		{"/v1/skills/orders:restore", `{"base_revision":1}`, http.StatusConflict, "skill_changed"},
		{"/v1/skills/orders:restore", `{"base_revision":2}`, http.StatusOK, `"revision":3`},
		{"/v1/skills/missing:retire", `{"base_revision":1}`, http.StatusNotFound, "skill_not_found"},
	} {
		rec := performClientRequest(router, http.MethodPost, tc.path, tc.body)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("POST %s %s = %d body %s", tc.path, tc.body, rec.Code, rec.Body.String())
		}
	}
	revisions, err := env.library.Revisions(t.Context(), "orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 3 || revisions[0].Retired || !revisions[1].Retired || revisions[0].Author != core.SkillAuthorOwner || revisions[1].Author != core.SkillAuthorOwner {
		t.Fatalf("revisions = %+v", revisions)
	}
}

// TestSkillSecretsAreWriteOnly sets and clears the secrets a package
// declares: responses name the secrets that are set, and no response or log
// line holds a value.
func TestSkillSecretsAreWriteOnly(t *testing.T) {
	env := newSkillTestEnv(t)
	env.importSkill(t,
		core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("github", "Read issues.", "Use the tools."))},
		core.SkillFile{Path: skills.ManifestFileName, Content: []byte("mcp:\n  command: npx\n  args: [github-mcp]\nsecrets: [GITHUB_TOKEN, GITHUB_ORG]\n")},
	)
	env.importSkill(t, core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown("notes", "Write notes.", "Write them."))})
	var logs bytes.Buffer
	router := newTestRouterForServer(&Server{skills: env.service, logger: slog.New(slog.NewTextHandler(&logs, nil))})
	const value = "ghp_secret_value_8c1f"

	var responses []string
	check := func(method, path, body string, status int) *SkillSecretListResponse {
		t.Helper()
		rec := performClientRequest(router, method, path, body)
		responses = append(responses, rec.Body.String())
		if rec.Code != status {
			t.Fatalf("%s %s = %d body %s", method, path, rec.Code, rec.Body.String())
		}
		if status != http.StatusOK || method == http.MethodGet {
			return nil
		}
		var out SkillSecretListResponse
		decodeClientTestJSON(t, rec, &out)
		return &out
	}
	check(http.MethodGet, "/v1/skills/github", "", http.StatusOK)
	if set := check(http.MethodPut, "/v1/skills/github/secrets/GITHUB_TOKEN", `{"value":"`+value+`"}`, http.StatusOK); !slices.Equal(set.Secrets, []SkillSecretDTO{{Name: "GITHUB_TOKEN", Set: true}, {Name: "GITHUB_ORG", Set: false}}) {
		t.Fatalf("after setting = %+v", set.Secrets)
	}
	check(http.MethodPut, "/v1/skills/github/secrets/OTHER_TOKEN", `{"value":"`+value+`"}`, http.StatusBadRequest)
	check(http.MethodPut, "/v1/skills/github/secrets/GITHUB_ORG", `{"value":""}`, http.StatusBadRequest)
	check(http.MethodPut, "/v1/skills/github/secrets/GITHUB_ORG", `{"value":"`+strings.Repeat("x", skills.MaxSecretBytes+1)+`"}`, http.StatusBadRequest)
	check(http.MethodPut, "/v1/skills/notes/secrets/GITHUB_TOKEN", `{"value":"`+value+`"}`, http.StatusBadRequest)
	check(http.MethodPut, "/v1/skills/missing/secrets/GITHUB_TOKEN", `{"value":"`+value+`"}`, http.StatusNotFound)
	check(http.MethodGet, "/v1/skills", "", http.StatusOK)
	check(http.MethodGet, "/v1/skills/github", "", http.StatusOK)
	check(http.MethodGet, "/v1/skills/github/revisions/1", "", http.StatusOK)
	values, err := env.db.SkillSecrets(t.Context(), "github")
	if err != nil || values["GITHUB_TOKEN"] != value {
		t.Fatalf("stored secrets = %v, %v", values, err)
	}
	rec := performClientRequest(router, http.MethodGet, "/v1/skills/github", "")
	var detail SkillEnvelope
	decodeClientTestJSON(t, rec, &detail)
	if pkg := detail.Item.Package; pkg == nil || pkg.MCP == nil || pkg.MCP.CommandLine != "npx github-mcp" || pkg.MCP.State != "missing_secrets" || !slices.Equal(pkg.Secrets, []SkillSecretDTO{{Name: "GITHUB_TOKEN", Set: true}, {Name: "GITHUB_ORG", Set: false}}) || detail.Item.Eligible || !slices.Equal(detail.Item.DisabledReasons, []string{"missing_secrets:GITHUB_ORG"}) {
		t.Fatalf("package = %+v", detail.Item)
	}

	if cleared := check(http.MethodDelete, "/v1/skills/github/secrets/GITHUB_TOKEN", "", http.StatusOK); cleared.Secrets[0].Set {
		t.Fatalf("after clearing = %+v", cleared.Secrets)
	}
	if values, err := env.db.SkillSecrets(t.Context(), "github"); err != nil || len(values) != 0 {
		t.Fatalf("stored secrets after clearing = %v, %v", values, err)
	}
	for i, body := range responses {
		if strings.Contains(body, value) {
			t.Fatalf("response %d holds the value: %s", i, body)
		}
	}
	if strings.Contains(logs.String(), value) {
		t.Fatalf("a log line holds the value: %s", logs.String())
	}
}
