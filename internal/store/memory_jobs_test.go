package store

import (
	"context"
	"testing"
	"time"
)

func TestMemoryWorkFailsUntilItsOperationSucceedsAgain(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedMemorySource(t, s, "owner:a", "owner", "我喜欢清淡")
	seedMemorySource(t, s, "owner:b", "owner", "我住在新加坡")
	at := memoryTestNow
	for attempt := range 3 {
		job, err := s.ClaimMemoryJob(ctx, at, time.Minute, "extract")
		if err != nil || job == nil || job.ObjectID != "owner:a" {
			t.Fatalf("claim attempt %d: %+v %v", attempt, job, err)
		}
		if err := s.FailMemoryJob(ctx, *job, "model unavailable", at); err != nil {
			t.Fatal(err)
		}
		at = at.Add(time.Hour)
	}
	failing, err := s.FailingMemoryWork(ctx)
	if err != nil || len(failing) != 1 || failing[0].Kind != "extract" || failing[0].Failed != 1 || failing[0].LastError != "model unavailable" || !failing[0].Since.Equal(memoryTestNow.Add(2*time.Hour)) {
		t.Fatalf("failing = %+v, %v", failing, err)
	}

	job, err := s.ClaimMemoryJob(ctx, at, time.Minute, "extract")
	if err != nil || job == nil || job.ObjectID != "owner:b" {
		t.Fatalf("claim the other job: %+v %v", job, err)
	}
	if err := s.FinishMemoryJob(ctx, *job, "", at); err != nil {
		t.Fatal(err)
	}
	if failing, err := s.FailingMemoryWork(ctx); err != nil || len(failing) != 0 {
		t.Fatalf("failing = %+v, %v; extract succeeded since", failing, err)
	}
}

func TestV28UpgradeDatesTheJobsGivenUpOnEarlier(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	if _, err := raw.Exec(readLayout(t, 27) + `PRAGMA user_version = 27;
		INSERT INTO memory_jobs(id, operation, object_id, version, state, attempts, available_at, error, created_at)
		VALUES(7, 'embed', 'm_a', '1', 'failed', 3, '2026-10-23T02:03:00.000000000Z', 'voyage: 401', '2026-10-23T02:00:00.000000000Z');
		INSERT INTO memory_jobs(id, operation, object_id, version, state, available_at, created_at, completed_at)
		VALUES(8, 'extract', 'owner:a', '1', 'done', '2026-10-23T02:00:00.000000000Z', '2026-10-23T02:00:00.000000000Z', '2026-10-23T02:01:00.000000000Z');`); err != nil {
		t.Fatalf("build v27: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	failing, err := s.FailingMemoryWork(context.Background())
	if err != nil || len(failing) != 1 || failing[0].Kind != "embed" || failing[0].LastError != "voyage: 401" || !failing[0].Since.Equal(time.Date(2026, 10, 23, 2, 3, 0, 0, time.UTC)) {
		t.Fatalf("failing after upgrade = %+v, %v", failing, err)
	}
	var next int64
	if err := s.db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'memory_jobs'`).Scan(&next); err != nil || next != 8 {
		t.Fatalf("memory_jobs sequence = %d, %v; the ids carry over", next, err)
	}
}
