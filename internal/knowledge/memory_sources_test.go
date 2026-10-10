package knowledge

import (
	"context"
	"errors"
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
