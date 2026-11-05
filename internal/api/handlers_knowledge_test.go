package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

type knowledgeFake struct {
	query, prefix string
	limit         int
	steps         []string
	deleteErr     error
}

var knowledgeFakeTime = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

func (k *knowledgeFake) Search(_ context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	k.query, k.limit = query, limit
	return []core.KnowledgeHit{{Path: "inbox/tokio.md", Title: "Tokio", Snippet: "…runtime…", UpdatedAt: knowledgeFakeTime}}, nil
}

func (k *knowledgeFake) Recent(_ context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	k.prefix, k.limit = prefix, limit
	return nil, nil
}

func (k *knowledgeFake) Read(_ context.Context, path string) (core.KnowledgeNote, error) {
	if _, err := knowledge.CleanNotePath(path); err != nil {
		return core.KnowledgeNote{}, err
	}
	if path != "inbox/tokio.md" {
		return core.KnowledgeNote{}, fmt.Errorf("%w: %s", core.ErrKnowledgeNoteNotFound, path)
	}
	return core.KnowledgeNote{Path: path, Title: "Tokio", Source: "https://tokio.rs", Revision: 3, CreatedAt: knowledgeFakeTime, UpdatedAt: knowledgeFakeTime, Body: "Async runtime."}, nil
}

func (k *knowledgeFake) ForgetNoteForOwner(_ context.Context, path, title string) (core.MemoryForgotten, error) {
	k.steps = append(k.steps, "forget "+path+" "+title)
	return core.MemoryForgotten{Exclusion: core.MemoryExclusion{Epoch: 7}}, nil
}

func (k *knowledgeFake) barrier(_ context.Context, exclusion core.MemoryExclusion, _ string) error {
	k.steps = append(k.steps, fmt.Sprintf("barrier %d", exclusion.Epoch))
	return nil
}

func (k *knowledgeFake) Delete(_ context.Context, path string, revision int64) error {
	k.steps = append(k.steps, fmt.Sprintf("delete %s %d", path, revision))
	return k.deleteErr
}

func newKnowledgeRouter(t *testing.T) (*knowledgeFake, http.Handler) {
	t.Helper()
	kf := &knowledgeFake{}
	knowledgeService, err := NewKnowledgeService(kf, kf, kf.barrier)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		knowledge:  knowledgeService,
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)
	return kf, router
}

func TestKnowledgeEndpoints(t *testing.T) {
	kf, router := newKnowledgeRouter(t)
	rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/notes?q=tokio&limit=5", "")
	if rec.Code != http.StatusOK || kf.query != "tokio" || kf.limit != 5 {
		t.Fatalf("search status %d query %q limit %d body=%s", rec.Code, kf.query, kf.limit, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tags":[]`) || !strings.Contains(rec.Body.String(), `"updated_at":"2026-10-03T01:00:00Z"`) {
		t.Fatalf("search body = %s", rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/knowledge/notes?prefix=inbox/", "")
	if rec.Code != http.StatusOK || kf.prefix != "inbox/" || kf.limit != defaultKnowledgeListLimit || rec.Body.String() != `{"notes":[]}` {
		t.Fatalf("list status %d prefix %q limit %d body=%s", rec.Code, kf.prefix, kf.limit, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/notes?limit=101", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit 101 status = %d", rec.Code)
	}

	rec = performClientRequest(router, http.MethodGet, "/v1/knowledge/note?path=inbox/tokio.md", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"Async runtime."`) || !strings.Contains(rec.Body.String(), `"created_at":"2026-10-03T01:00:00Z"`) {
		t.Fatalf("read status %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/note?path=inbox/missing.md", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "knowledge_note_not_found") {
		t.Fatalf("missing status %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/note?path=../x.md", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("escape status = %d", rec.Code)
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodGet, "/v1/knowledge/notes", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}

func TestDeleteKnowledgeNoteForgetsItBeforeRemovingIt(t *testing.T) {
	kf, router := newKnowledgeRouter(t)
	rec := performClientRequest(router, http.MethodDelete, "/v1/knowledge/note?path=inbox/tokio.md", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := []string{"forget inbox/tokio.md Tokio", "barrier 7", "delete inbox/tokio.md 3"}
	if strings.Join(kf.steps, "|") != strings.Join(want, "|") {
		t.Fatalf("steps = %q, want %q", kf.steps, want)
	}
}

func TestDeleteKnowledgeNoteReportsMissingInvalidAndChangedNotes(t *testing.T) {
	for _, tc := range []struct {
		path      string
		deleteErr error
		status    int
		code      string
	}{
		{path: "inbox/missing.md", status: http.StatusNotFound, code: "knowledge_note_not_found"},
		{path: "../secrets.md", status: http.StatusBadRequest, code: "invalid_request"},
		{path: "inbox/tokio.md", deleteErr: fmt.Errorf("%w: inbox/tokio.md is at revision 4, not 3", core.ErrKnowledgeConflict), status: http.StatusConflict, code: "knowledge_note_conflict"},
	} {
		kf, router := newKnowledgeRouter(t)
		kf.deleteErr = tc.deleteErr
		rec := performClientRequest(router, http.MethodDelete, "/v1/knowledge/note?path="+tc.path, "")
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Fatalf("%s: status = %d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if tc.status != http.StatusConflict && len(kf.steps) != 0 {
			t.Fatalf("%s: a note that cannot be read must not be forgotten: %q", tc.path, kf.steps)
		}
	}
}
