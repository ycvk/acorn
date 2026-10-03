package store

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestKnowledgeIndexSearch(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	notes := []core.KnowledgeNote{
		{Path: "inbox/rust-async.md", Title: "Rust async runtime", Tags: []string{"rust", "async"}, Body: "Tokio 是最常用的异步运行时,work stealing 调度。", MTimeNS: 1, Size: 10, UpdatedAt: day},
		{Path: "reading/go.md", Title: "Go 1.27 release notes", Tags: []string{"go"}, Body: "Generic methods landed.", MTimeNS: 2, Size: 20, UpdatedAt: day.Add(time.Hour)},
	}
	for _, n := range notes {
		if err := s.UpsertKnowledgeNote(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.Path, err)
		}
	}
	for query, want := range map[string]string{"异步运行时": "inbox/rust-async.md", "Generic methods": "reading/go.md", "调度": "inbox/rust-async.md", "go": "reading/go.md"} {
		hits, err := s.SearchKnowledge(ctx, query, 5)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if len(hits) == 0 || hits[0].Path != want {
			t.Fatalf("search %q = %+v, want first %s", query, hits, want)
		}
		if hits[0].Snippet == "" || hits[0].UpdatedAt.IsZero() {
			t.Fatalf("search %q hit lacks snippet or time: %+v", query, hits[0])
		}
	}

	replaced := notes[0]
	replaced.Body = "换成了 smol。"
	replaced.MTimeNS = 3
	if err := s.UpsertKnowledgeNote(ctx, replaced); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if hits, err := s.SearchKnowledge(ctx, "异步运行时", 5); err != nil || len(hits) != 0 {
		t.Fatalf("old body still matches: %+v, %v", hits, err)
	}
	if hits, err := s.SearchKnowledge(ctx, "smol", 5); err != nil || len(hits) != 1 || hits[0].Tags[0] != "rust" {
		t.Fatalf("new body search = %+v, %v", hits, err)
	}

	if err := s.DeleteKnowledgeNote(ctx, "reading/go.md"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if hits, err := s.SearchKnowledge(ctx, "Generic methods", 5); err != nil || len(hits) != 0 {
		t.Fatalf("deleted note still matches: %+v, %v", hits, err)
	}
	stats, err := s.ListKnowledgeFileStats(ctx)
	if err != nil || len(stats) != 1 || stats[0] != (core.KnowledgeFileStat{Path: "inbox/rust-async.md", MTimeNS: 3, Size: 10}) {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
}

func TestRecentKnowledgeOrdersAndFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	for i, path := range []string{"inbox/a.md", "inbox/b.md", "projects/c.md"} {
		if err := s.UpsertKnowledgeNote(ctx, core.KnowledgeNote{Path: path, Title: path, Body: "## body\n\nof " + path, MTimeNS: 1, Size: 1, UpdatedAt: day.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatalf("upsert: %v", err)
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
	if err := s.UpsertKnowledgeNote(ctx, core.KnowledgeNote{Path: "x.md"}); err == nil {
		t.Fatal("a note without updated_at must be rejected")
	}
}
