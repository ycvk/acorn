package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

var shanghai = time.FixedZone("CST", 8*3600)

type testVault struct {
	*Vault
	db  *store.Store
	dir string
	now time.Time
}

func openTestVault(t *testing.T) *testVault {
	t.Helper()
	tv := &testVault{dir: t.TempDir(), now: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)}
	db, err := store.Open(tv.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tv.db = db
	v, err := NewVault(VaultConfig{Store: db, StorageDir: tv.dir, Clock: func() time.Time { return tv.now }, Location: shanghai})
	if err != nil {
		t.Fatal(err)
	}
	tv.Vault = v
	return tv
}

func TestCleanNotePath(t *testing.T) {
	for _, bad := range []string{"", "../x.md", "/abs.md", ".git/config", "a.txt", "notes/../x.md", "./x.md", `a\b.md`, ".obsidian/x.md", "attachments/x.md", "a//b.md"} {
		if _, err := CleanNotePath(bad); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanNotePath(%q) err = %v, want ErrInvalidPath", bad, err)
		}
	}
	if got, err := CleanNotePath(" inbox/读书.md "); err != nil || got != "inbox/读书.md" {
		t.Fatalf("CleanNotePath = %q, %v", got, err)
	}
}

func TestWriteAddsRevisionsAndKeepsCreated(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	created := v.now
	first, err := v.Write(ctx, WriteNote{Path: "inbox/rust.md", Title: " Rust 异步 ", Tags: []string{"#rust", "rust", "async"}, Source: "https://example.com", Body: "\n第一版\n", RunID: "run-1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.Title != "Rust 异步" || strings.Join(first.Tags, ",") != "rust,async" || first.Body != "第一版" {
		t.Fatalf("first write = %+v", first)
	}
	v.now = v.now.Add(time.Hour)
	second, err := v.Write(ctx, WriteNote{Path: "inbox/rust.md", Title: "Rust 异步", Body: "第二版", RunID: "run-2"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 || !second.CreatedAt.Equal(created) || !second.UpdatedAt.Equal(v.now) {
		t.Fatalf("second write = %+v", second)
	}
	read, err := v.Read(ctx, "inbox/rust.md")
	if err != nil || read.Body != "第二版" || read.Revision != 2 || read.Source != "" {
		t.Fatalf("read = %+v, %v", read, err)
	}
	for revision, body := range map[int64]string{1: "第一版", 2: "第二版"} {
		source, err := v.db.LoadMemorySource(ctx, core.KnowledgeSourceID("inbox/rust.md", revision))
		if err != nil || source.Content != body || source.Speaker != "assistant" {
			t.Fatalf("revision %d source = %+v, %v", revision, source, err)
		}
	}
	if _, err := v.Read(ctx, "missing.md"); !errors.Is(err, core.ErrKnowledgeNoteNotFound) {
		t.Fatalf("missing note err = %v", err)
	}
	if _, err := v.Write(ctx, WriteNote{Path: "x.md", Title: "x", Body: strings.Repeat("a", MaxNoteBodyBytes+1)}); err == nil {
		t.Fatal("oversized body accepted")
	}
}

func TestEditNeedsUniqueOldTextAndCurrentRevision(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	if _, err := v.Write(ctx, WriteNote{Path: "a.md", Title: "A", Tags: []string{"t"}, Body: "one two two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Edit(ctx, "a.md", "three", "3", ""); err == nil || !strings.Contains(err.Error(), "does not occur") {
		t.Fatalf("missing old text err = %v", err)
	}
	if _, err := v.Edit(ctx, "a.md", "two", "2", ""); err == nil || !strings.Contains(err.Error(), "occurs 2 times") {
		t.Fatalf("ambiguous old text err = %v", err)
	}
	edited, err := v.Edit(ctx, "a.md", "one", "1", "run-edit")
	if err != nil || edited.Body != "1 two two" || edited.Revision != 2 || edited.Title != "A" || strings.Join(edited.Tags, ",") != "t" {
		t.Fatalf("edit = %+v, %v", edited, err)
	}
	_, err = v.db.WriteKnowledgeNote(ctx, core.KnowledgeWrite{Path: "a.md", Title: "A", Body: "stale", At: v.now, BaseRevision: 1})
	if !errors.Is(err, core.ErrKnowledgeConflict) {
		t.Fatalf("stale base revision err = %v", err)
	}
}

func TestSearchAndRecentCarryRevisions(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	if _, err := v.Write(ctx, WriteNote{Path: "inbox/a.md", Title: "甲", Body: "关于香菜的记录"}); err != nil {
		t.Fatal(err)
	}
	v.now = v.now.Add(time.Minute)
	if _, err := v.Write(ctx, WriteNote{Path: "inbox/a.md", Title: "甲", Body: "关于香菜的新记录"}); err != nil {
		t.Fatal(err)
	}
	hits, err := v.Search(ctx, "香菜的", 10)
	if err != nil || len(hits) != 1 || hits[0].Revision != 2 {
		t.Fatalf("search = %+v, %v", hits, err)
	}
	recent, err := v.Recent(ctx, "inbox/", 10)
	if err != nil || len(recent) != 1 || recent[0].Revision != 2 {
		t.Fatalf("recent = %+v, %v", recent, err)
	}
	status, err := v.Status(ctx)
	if err != nil || status.Notes != 1 || status.Attachments != filepath.Join(v.dir, "attachments") {
		t.Fatalf("status = %+v, %v", status, err)
	}
}

func TestSaveAttachment(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	rel, err := v.SaveAttachment(ctx, "image/png", []byte("\x89PNG fake"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "attachments/2026/10/") || !strings.HasSuffix(rel, ".png") {
		t.Fatalf("path = %q", rel)
	}
	if data, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(rel))); err != nil || string(data) != "\x89PNG fake" {
		t.Fatalf("stored attachment = %q, %v", data, err)
	}
	if _, err := v.SaveAttachment(ctx, "application/pdf", []byte("x")); err == nil {
		t.Fatal("unsupported type must be rejected")
	}
}

func TestReadAttachmentServesOnlySavedImages(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	rel, err := v.SaveAttachment(ctx, "image/jpeg", []byte("\xff\xd8 fake"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.ReadAttachment(ctx, rel)
	if err != nil || got.MIME != "image/jpeg" || string(got.Data) != "\xff\xd8 fake" {
		t.Fatalf("read = %+v, %v", got, err)
	}
	missing := strings.Replace(rel, rel[len("attachments/2026/10/"):len("attachments/2026/10/")+16], "0123456789abcdef", 1)
	if _, err := v.ReadAttachment(ctx, missing); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("missing attachment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(v.dir, "acorn.secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"acorn.secret", "attachments/../acorn.secret", "attachments/2026/10/" + "0123456789abcdef.md", "/" + rel, rel + "/"} {
		if _, err := v.ReadAttachment(ctx, bad); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("ReadAttachment(%q) = %v, want invalid path", bad, err)
		}
	}
}
