package knowledge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

func TestKnowledgeMemoryVersionsAndDerivedExclusion(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	dir := filepath.Join(t.TempDir(), "vault")
	git, err := LookupGit()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.TempDir(), store.OpenOptions{SourceReader: &SourceReader{Git: git, Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := Open(ctx, VaultConfig{Dir: dir, Git: git, Index: db, Memory: db, Clock: func() time.Time { return now }, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	first, err := v.Write(ctx, WriteNote{Path: "preferences.md", Title: "饮食", Body: "不吃香菜"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := v.Write(ctx, WriteNote{Path: "preferences.md", Title: "饮食", Body: "现在可以吃少量香菜"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.LoadMemorySource(ctx, knowledgeSourceID(first.Path, first.Commit))
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.LoadMemorySource(ctx, knowledgeSourceID(second.Path, second.Commit))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.Content, "不吃香菜") || strings.Contains(a.Content, "少量") || !strings.Contains(b.Content, "少量") || a.Body != "" {
		t.Fatalf("incorrect version resolution: %+v %+v", a, b)
	}
	if err := v.SyncMemory(ctx, db); err != nil {
		t.Fatal(err)
	}
	pending, count, err := db.PendingMemorySources(ctx, core.MemoryQuery{})
	if err != nil || count != 2 || len(pending) != 2 {
		t.Fatalf("replay duplicated sources: %d %d %v", count, len(pending), err)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:forget", Kind: "standalone", ObjectID: "forget", Version: "1", Speaker: "owner", Body: "忘掉饮食偏好", RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMemorySnapshotRefs(ctx, "snapshot", "run-derived", []string{a.ID}, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: now}); err != nil {
		t.Fatal(err)
	}
	derived, err := v.Write(ctx, WriteNote{Path: "summary.md", Title: "总结", Body: "用户不吃香菜", RunID: "run-derived"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.LoadMemorySource(ctx, knowledgeSourceID(derived.Path, derived.Commit))
	if !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("late derived note must inherit exclusion: %v", err)
	}
	view := &MemoryView{Vault: v, Store: db}
	if _, err := view.Read(ctx, "summary.md"); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("agent read returned excluded note: %v", err)
	}
	if note, err := v.Read(ctx, "summary.md"); err != nil || strings.TrimSpace(note.Body) != "用户不吃香菜" {
		t.Fatalf("owner file changed: %+v %v", note, err)
	}
	hits, err := view.Search(ctx, "香菜", 10)
	if err != nil || len(hits) != 1 || hits[0].Path != "preferences.md" {
		t.Fatalf("agent search: %+v %v", hits, err)
	}
	// A later owner revision is an independently attributed source.
	if _, err = db.LoadMemorySource(ctx, b.ID); err != nil {
		t.Fatalf("independent revision hidden: %v", err)
	}
}
