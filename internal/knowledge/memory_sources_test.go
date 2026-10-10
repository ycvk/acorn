package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestKnowledgeRevisionsAreSourcesAndDerivedExclusion(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	db := v.db
	first, err := v.Write(ctx, WriteNote{Path: "preferences.md", Title: "饮食", Body: "不吃香菜"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := v.Write(ctx, WriteNote{Path: "preferences.md", Title: "饮食", Body: "现在可以吃少量香菜"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.LoadMemorySource(ctx, core.KnowledgeSourceID(first.Path, first.Revision))
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.LoadMemorySource(ctx, core.KnowledgeSourceID(second.Path, second.Revision))
	if err != nil {
		t.Fatal(err)
	}
	if a.Content != "不吃香菜" || b.Content != "现在可以吃少量香菜" || a.Body != "" || a.Speaker != "external" {
		t.Fatalf("incorrect version resolution: %+v %+v", a, b)
	}
	pending, count, err := db.PendingMemorySources(ctx, core.MemoryQuery{})
	if err != nil || count != 2 || len(pending) != 2 {
		t.Fatalf("pending sources: %d %d %v", count, len(pending), err)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:forget", Kind: "standalone", ObjectID: "forget", Version: "1", Speaker: "owner", Body: "忘掉饮食偏好", RecordedAt: v.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMemorySnapshotRefs(ctx, "snapshot", "run-derived", []string{a.ID}, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: v.now}); err != nil {
		t.Fatal(err)
	}
	derived, err := v.Write(ctx, WriteNote{Path: "summary.md", Title: "总结", Body: "用户不吃香菜", RunID: "run-derived"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoadMemorySource(ctx, core.KnowledgeSourceID(derived.Path, derived.Revision)); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("late derived note must inherit exclusion: %v", err)
	}
	view := &MemoryView{Vault: v.Vault, Store: db}
	if _, err := view.Read(ctx, "summary.md"); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("agent read returned excluded note: %v", err)
	}
	if note, err := v.Read(ctx, "summary.md"); err != nil || note.Body != "用户不吃香菜" {
		t.Fatalf("owner read changed: %+v %v", note, err)
	}
	hits, err := view.Search(ctx, "香菜", 10)
	if err != nil || len(hits) != 1 || hits[0].Path != "preferences.md" {
		t.Fatalf("agent search: %+v %v", hits, err)
	}
	// A later revision is an independently attributed source.
	if _, err = db.LoadMemorySource(ctx, b.ID); err != nil {
		t.Fatalf("independent revision hidden: %v", err)
	}
}

func TestImportLegacyDirMovesNotesIntoRevisions(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	legacy := filepath.Join(v.dir, "knowledge")
	for _, dir := range []string{".git", "briefings", "attachments/2026/10"} {
		if err := os.MkdirAll(filepath.Join(legacy, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	note := "---\ntitle: 早安卡\ntags: [briefing, 日报]\ncreated: 2026-10-09T08:00:00+08:00\nupdated: 2026-10-09T08:05:00+08:00\n---\n\n今天天气晴\n"
	files := map[string]string{"briefings/2026-10-09.md": note, "plain.md": "# 标题来自正文\n\n内容", "attachments/2026/10/a.png": "png", ".git/HEAD": "ref"}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(legacy, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The former git era registered the briefing under its commit.
	raw, err := sql.Open("sqlite", filepath.Join(v.dir, "acorn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `INSERT INTO memory_sources(id,kind,object_id,version,speaker,run_id,recorded_at) VALUES('knowledge:legacy','knowledge','briefings/2026-10-09.md','0123456789abcdef0123456789abcdef01234567','assistant','run-briefing','2026-10-09T00:05:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := ImportLegacyDir(ctx, v.dir, v.db, shanghai); err != nil {
		t.Fatal(err)
	}
	got, err := v.Read(ctx, "briefings/2026-10-09.md")
	if err != nil || got.Revision != 1 || got.Title != "早安卡" || len(got.Tags) != 2 || got.Body != "今天天气晴" || got.CreatedAt.Hour() != 0 {
		t.Fatalf("imported note = %+v, %v", got, err)
	}
	if plain, err := v.Read(ctx, "plain.md"); err != nil || plain.Title != "标题来自正文" {
		t.Fatalf("imported plain note = %+v, %v", plain, err)
	}
	loaded, err := v.db.LoadMemorySource(ctx, "knowledge:legacy")
	if err != nil || loaded.Version != "1" || loaded.Content != "今天天气晴" {
		t.Fatalf("legacy source = %+v, %v", loaded, err)
	}
	if data, err := os.ReadFile(filepath.Join(v.dir, "attachments/2026/10/a.png")); err != nil || string(data) != "png" {
		t.Fatalf("attachment not moved: %q %v", data, err)
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy dir remains: %v", err)
	}
	if err := ImportLegacyDir(ctx, v.dir, v.db, shanghai); err != nil {
		t.Fatalf("second import: %v", err)
	}
}
