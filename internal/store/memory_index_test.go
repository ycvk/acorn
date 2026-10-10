package store

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestMemoryReindexLockResumeAndGeneration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir, OpenOptions{Exclusive: true}); err == nil {
		other.Close()
		t.Fatal("exclusive maintenance opened active store")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dir, OpenOptions{Exclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if other, err := Open(dir); err == nil {
		other.Close()
		t.Fatal("serve opened during maintenance")
	}
	spec := core.MemoryIndex{Model: "voyage-4", Dimensions: 2}
	index, err := db.ConfigureMemoryIndex(ctx, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	source := seedMemorySource(t, db, "owner:reindex", "owner", "我喜欢清淡食物")
	records, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "喜欢清淡食物")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := db.ConfigureMemoryIndex(ctx, spec, true)
	if err != nil || rebuilt.Generation != index.Generation+1 || rebuilt.State != "rebuilding" {
		t.Fatalf("rebuild %+v %v", rebuilt, err)
	}
	resumed, err := db.ConfigureMemoryIndex(ctx, spec, true)
	if err != nil || resumed != rebuilt {
		t.Fatalf("resume %+v %v", resumed, err)
	}
	if err := db.CompleteMemoryIndex(ctx, rebuilt.Generation); err == nil {
		t.Fatal("incomplete generation became ready")
	}
	job, err := db.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "embed")
	if err != nil || job == nil || job.Operation != "embed" {
		t.Fatalf("job %+v %v", job, err)
	}
	if err := db.SaveMemoryVectors(ctx, *job, []core.MemoryVector{{ID: records[0].ID, Revision: 1, Generation: rebuilt.Generation, Values: []float32{1, 0}}}, memoryTestNow); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteMemoryIndex(ctx, rebuilt.Generation); err != nil {
		t.Fatal(err)
	}
	got, err := db.MemoryIndex(ctx)
	if err != nil || got.State != "ready" {
		t.Fatalf("index %+v %v", got, err)
	}
}

func TestMemoryRevisionRetainsItsHistoricalVectorAtomically(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "fixture", Dimensions: 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	source := seedMemorySource(t, db, "owner:vector-revision", "owner", "我喜欢清淡食物")
	change := memoryFact(source, "喜欢清淡食物")
	records, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "embed")
	if err != nil || job == nil {
		t.Fatalf("embedding job=%+v err=%v", job, err)
	}
	if err := db.SaveMemoryVectors(ctx, *job, []core.MemoryVector{{ID: records[0].ID, Revision: 1, Generation: index.Generation, Values: []float32{1, 0}}}, memoryTestNow); err != nil {
		t.Fatal(err)
	}
	change.Draft.ID = records[0].ID
	change.Draft.State = "disputed"
	change.ExpectedRevision = 1
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	var stale int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM memory_embeddings WHERE record_id=?`, records[0].ID).Scan(&stale); err != nil || stale != 1 {
		t.Fatalf("historical vector missing: n=%d err=%v", stale, err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM memory_vector_sketches WHERE record_id=?`, records[0].ID).Scan(&stale); err != nil || stale != 1 {
		t.Fatalf("historical candidate index missing: n=%d err=%v", stale, err)
	}
}
