package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestReadPoolRejectsWrites(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.read.Exec(`INSERT INTO memory_entities(name) VALUES('owner')`); err == nil {
		t.Fatal("read pool accepted a write")
	}
}

func TestRecallReadsProceedWhileWriterIsBusy(t *testing.T) {
	s := openTestStore(t)
	seedCapacityCorpus(t, s, 64, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_entities(name) VALUES('pending writer')`); err != nil {
			return err
		}
		if _, err := s.MemoryProcessingStatus(ctx); err != nil {
			return err
		}
		if _, err := s.MemoryExclusions(ctx); err != nil {
			return err
		}
		hits, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "topic03", Limit: 5})
		if err != nil {
			return err
		}
		if len(hits) == 0 {
			t.Error("committed records are invisible to the read pool")
		}
		sketches, err := s.MemoryVectorSketches(ctx, 1, "", 512, time.Time{})
		if err != nil {
			return err
		}
		if len(sketches) != 64 {
			t.Errorf("vector page has %d sketches", len(sketches))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
