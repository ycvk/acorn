package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

func TestTheOwnerSeesThePortraitWithItsEvidenceAndRemovesALine(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := t.Context()
	now := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	long := "我最近在用 Go 写一个叫 Acorn 的个人 agent，" + strings.Repeat("细节", 100)
	for _, source := range []core.MemorySource{
		{ID: "owner:a", Kind: "import", ObjectID: "owner:a", Version: "1", Speaker: "owner", SessionID: "thread_1", Body: long, RecordedAt: now.Add(-2 * time.Hour)},
		{ID: "owner:b", Kind: "import", ObjectID: "owner:b", Version: "1", Speaker: "owner", SessionID: "thread_2", Body: "展开讲讲：Anthropic   提交上市文件", RecordedAt: now.Add(-time.Hour)},
	} {
		if _, err := db.RegisterMemorySource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueuePortrait(ctx, "day:2026-11-05", now); err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimMemoryJob(ctx, now, time.Minute, core.PortraitOperation)
	if err != nil || job == nil {
		t.Fatalf("claim = %+v, %v", job, err)
	}
	if err := db.CommitPortrait(ctx, core.PortraitMutation{Lease: *job, Now: now, Lines: []core.PortraitDraft{
		{Content: "在用 Go 写个人 agent Acorn", SourceIDs: []string{"owner:a"}},
		{Content: "关注 AI 公司动态", SourceIDs: []string{"owner:b", "owner:a"}},
	}}); err != nil {
		t.Fatal(err)
	}
	portrait, err := NewPortraitService(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		portrait:   portrait,
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodGet, "/v1/portrait", "")
	var got PortraitDTO
	decodeClientTestJSON(t, rec, &got)
	if rec.Code != http.StatusOK || len(got.Lines) != 2 || got.UpdatedAt == nil || !got.UpdatedAt.Equal(now) {
		t.Fatalf("portrait = %d %+v", rec.Code, got)
	}
	second := got.Lines[1]
	if second.Content != "关注 AI 公司动态" || len(second.Evidence) != 2 {
		t.Fatalf("second line = %+v", second)
	}
	if e := second.Evidence[0]; e.Excerpt != "展开讲讲：Anthropic 提交上市文件" || e.ThreadID != "thread_2" || e.SourceKind != "import" || !e.RecordedAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("first evidence = %+v", e)
	}
	if excerpt := []rune(second.Evidence[1].Excerpt); len(excerpt) != portraitExcerptRunes+1 || excerpt[len(excerpt)-1] != '…' {
		t.Fatalf("long evidence excerpt = %q", string(excerpt))
	}

	path := "/v1/portrait/lines/" + strconv.FormatInt(got.Lines[0].ID, 10)
	if rec := performClientRequest(router, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodDelete, path, ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "portrait_line_not_found") {
		t.Fatalf("second remove = %d %s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodDelete, "/v1/portrait/lines/first", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("remove with a malformed id = %d %s", rec.Code, rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/portrait", "")
	decodeClientTestJSON(t, rec, &got)
	if len(got.Lines) != 1 || got.Lines[0].Content != "关注 AI 公司动态" {
		t.Fatalf("portrait after removing a line = %+v", got)
	}
}
