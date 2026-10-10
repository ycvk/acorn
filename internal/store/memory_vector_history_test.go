package store

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func historyVectorRevisions(t *testing.T, db *Store) (core.MemoryRecord, core.MemorySource, core.MemorySource) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "fixture", Dimensions: 2}, false); err != nil {
		t.Fatal(err)
	}
	first := seedMemorySource(t, db, "first", "owner", "先独自尝试")
	change := memoryFact(first, first.Content)
	records, err := db.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow, Changes: []core.MemoryChange{change}})
	if err != nil {
		t.Fatal(err)
	}
	second := seedMemorySource(t, db, "second", "owner", "再与同伴讨论")
	change = memoryFact(second, second.Content)
	change.Draft.ID, change.ExpectedRevision = records[0].ID, 1
	revised, err := db.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow.Add(time.Minute), Changes: []core.MemoryChange{change}})
	if err != nil {
		t.Fatal(err)
	}
	for revision := int64(1); revision <= 2; revision++ {
		job, err := db.ClaimMemoryJob(ctx, memoryTestNow.Add(time.Minute), time.Minute, "embed")
		if err != nil || job == nil {
			t.Fatalf("job=%+v err=%v", job, err)
		}
		values := []float32{1, 0}
		if revision == 2 {
			values = []float32{0, 1}
		}
		if err := db.SaveMemoryVectors(ctx, *job, []core.MemoryVector{{ID: revised[0].ID, Revision: revision, Generation: 1, Values: values}}, memoryTestNow.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	return revised[0], first, second
}

func TestMemoryHistoricalVectorsAreRemovedWhenTheirEvidenceIsForgotten(t *testing.T) {
	db := openTestStore(t)
	record, first, _ := historyVectorRevisions(t, db)
	request := seedMemorySource(t, db, "forget", "owner", "忘记之前的独处偏好")
	ctx := context.Background()
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{first.ID}, RequestSourceID: request.ID, Reason: request.Content, Now: memoryTestNow.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	read, err := db.ReadMemory(ctx, record.ID)
	if err != nil || len(read.Versions) != 1 || read.Versions[0].Revision != 2 {
		t.Fatalf("surviving revisions=%+v %v", read.Versions, err)
	}
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM memory_embeddings WHERE record_id=? AND revision=1`, record.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("excluded historical vector retained: %d %v", count, err)
	}
	past, err := db.MemoryVectorSketches(ctx, 1, "", 512, memoryTestNow)
	if err != nil || len(past) != 0 {
		t.Fatalf("past=%+v %v", past, err)
	}
	if err := db.CompleteMemoryIndex(ctx, 1); err != nil {
		t.Fatal(err)
	}
	index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "fixture", Dimensions: 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimMemoryJob(ctx, memoryTestNow.Add(3*time.Minute), time.Minute, "embed")
	if err != nil || job == nil || job.Version != "2" {
		t.Fatalf("reindex included forgotten history: %+v %v", job, err)
	}
	if err := db.SaveMemoryVectors(ctx, *job, []core.MemoryVector{{ID: record.ID, Revision: 2, Generation: index.Generation, Values: []float32{0, 1}}}, memoryTestNow.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteMemoryIndex(ctx, index.Generation); err != nil {
		t.Fatal(err)
	}
}
