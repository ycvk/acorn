package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/textedit"
)

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
	v, err := NewVault(VaultConfig{Store: db, Clock: func() time.Time { return tv.now }})
	if err != nil {
		t.Fatal(err)
	}
	tv.Vault = v
	return tv
}

func TestCleanNotePath(t *testing.T) {
	for _, bad := range []string{"", "../x.md", "/abs.md", ".git/config", "a.txt", "notes/../x.md", "./x.md", `a\b.md`, ".obsidian/x.md", "a//b.md"} {
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
	if _, err := v.Write(ctx, WriteNote{Path: "a.md", Title: "A", Tags: []string{"t"}, Body: "one two two\nthree"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Edit(ctx, "a.md", []textedit.Edit{{Old: "four", New: "4"}}, ""); err == nil || !strings.Contains(err.Error(), "is not in the file") {
		t.Fatalf("missing old text err = %v", err)
	}
	if _, err := v.Edit(ctx, "a.md", []textedit.Edit{{Old: "one", New: "1"}, {Old: "two", New: "2"}}, ""); err == nil || !strings.Contains(err.Error(), "edit 2: the old text occurs 2 times") {
		t.Fatalf("ambiguous old text err = %v", err)
	}
	edited, err := v.Edit(ctx, "a.md", []textedit.Edit{{Old: "one", New: "1"}, {Old: "three", New: "3"}}, "run-edit")
	if err != nil || edited.Body != "1 two two\n3" || edited.Revision != 2 || edited.Title != "A" || strings.Join(edited.Tags, ",") != "t" {
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
	if err != nil || status.Notes != 1 {
		t.Fatalf("status = %+v, %v", status, err)
	}
}
