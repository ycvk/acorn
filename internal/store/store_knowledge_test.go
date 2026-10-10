package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestKnowledgeSearchFollowsCurrentRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	writes := []core.KnowledgeWrite{
		{Path: "inbox/rust-async.md", Title: "Rust async runtime", Tags: []string{"rust", "async"}, Body: "Tokio 是最常用的异步运行时,work stealing 调度。", At: day},
		{Path: "reading/go.md", Title: "Go 1.27 release notes", Tags: []string{"go"}, Body: "Generic methods landed.", At: day.Add(time.Hour)},
	}
	for _, w := range writes {
		if _, err := s.WriteKnowledgeNote(ctx, w); err != nil {
			t.Fatalf("write %s: %v", w.Path, err)
		}
	}
	for query, want := range map[string]string{"异步运行时": "inbox/rust-async.md", "Generic methods": "reading/go.md", "调度": "inbox/rust-async.md", "go": "reading/go.md"} {
		hits, err := s.SearchKnowledge(ctx, query, 5)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if len(hits) == 0 || hits[0].Path != want || hits[0].Snippet == "" || hits[0].UpdatedAt.IsZero() || hits[0].Revision != 1 {
			t.Fatalf("search %q = %+v, want first %s", query, hits, want)
		}
	}
	replaced := writes[0]
	replaced.Body, replaced.At = "换成了 smol。", day.Add(2*time.Hour)
	note, err := s.WriteKnowledgeNote(ctx, replaced)
	if err != nil || note.Revision != 2 || !note.CreatedAt.Equal(day) {
		t.Fatalf("replace = %+v, %v", note, err)
	}
	if hits, err := s.SearchKnowledge(ctx, "异步运行时", 5); err != nil || len(hits) != 0 {
		t.Fatalf("old body still matches: %+v, %v", hits, err)
	}
	if hits, err := s.SearchKnowledge(ctx, "smol", 5); err != nil || len(hits) != 1 || hits[0].Tags[0] != "rust" || hits[0].Revision != 2 {
		t.Fatalf("new body search = %+v, %v", hits, err)
	}
	old, err := s.LoadMemorySource(ctx, core.KnowledgeSourceID("inbox/rust-async.md", 1))
	if err != nil || old.Content != writes[0].Body {
		t.Fatalf("revision 1 source = %+v, %v", old, err)
	}
	if n, err := s.CountKnowledgeNotes(ctx); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if _, err := s.WriteKnowledgeNote(ctx, core.KnowledgeWrite{Path: "inbox/rust-async.md", Title: "x", Body: "y", At: day, BaseRevision: 1}); !errors.Is(err, core.ErrKnowledgeConflict) {
		t.Fatalf("stale write err = %v", err)
	}
	if _, err := s.KnowledgeNote(ctx, "missing.md"); !errors.Is(err, core.ErrKnowledgeNoteNotFound) {
		t.Fatalf("missing note err = %v", err)
	}
}

func TestRecentKnowledgeOrdersAndFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	for i, path := range []string{"inbox/a.md", "inbox/b.md", "projects/c.md"} {
		if _, err := s.WriteKnowledgeNote(ctx, core.KnowledgeWrite{Path: path, Title: path, Body: "## body\n\nof " + path, At: day.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	all, err := s.RecentKnowledge(ctx, "", 10)
	if err != nil || len(all) != 3 || all[0].Path != "projects/c.md" || all[2].Path != "inbox/a.md" {
		t.Fatalf("recent = %+v, %v", all, err)
	}
	inbox, err := s.RecentKnowledge(ctx, "inbox/", 10)
	if err != nil || len(inbox) != 2 || inbox[0].Path != "inbox/b.md" || inbox[0].Snippet != "## body of inbox/b.md" {
		t.Fatalf("inbox = %+v, %v", inbox, err)
	}
	if _, err := s.RecentKnowledge(ctx, "", 0); err == nil {
		t.Fatal("a non-positive limit must be rejected")
	}
	if _, err := s.WriteKnowledgeNote(ctx, core.KnowledgeWrite{Path: "x.md", Title: "x"}); err == nil {
		t.Fatal("a write without a time must be rejected")
	}
}
