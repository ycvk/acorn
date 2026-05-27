package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/crystallization"
)

func TestCrystallizationIndexStoreUpsertQueryDelete(t *testing.T) {
	store := openCrystallizationIndexTestStore(t)
	defer closeCrystallizationIndexTestStore(t, store)

	ctx := context.Background()
	entry := &crystallization.IndexEntry{
		SkillID:      "test-skill",
		SkillName:    "Test Skill",
		Summary:      "A test skill",
		Keywords:     []string{"test", "skill"},
		TaskPattern:  "test pattern",
		QualityScore: 75,
		Source:       "test",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Query(ctx, "test pattern", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected query to return results")
	}

	if err := store.Delete(ctx, "test-skill"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err = store.Query(ctx, "test pattern", 10)
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

func TestCrystallizationIndexStoreRejectsInvalidStoredTime(t *testing.T) {
	store := openCrystallizationIndexTestStore(t)
	defer closeCrystallizationIndexTestStore(t, store)

	if _, err := store.db.ExecContext(context.Background(), `
		INSERT INTO insight_index (skill_id, skill_name, summary, keywords, task_pattern, quality_score, source, created_at, updated_at)
		VALUES ('bad-time', 'Bad Time', 'summary', 'bad,time', 'bad time', 50, 'test', 'not-a-time', '2026-05-10T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert bad index row: %v", err)
	}
	_, err := store.Query(context.Background(), "bad time", 10)
	if err == nil || !strings.Contains(err.Error(), "parse insight index created_at") {
		t.Fatalf("Query error = %v, want created_at parse error", err)
	}
}

func openCrystallizationIndexTestStore(t *testing.T) *CrystallizationIndexStore {
	t.Helper()
	store, err := OpenCrystallizationIndexStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenCrystallizationIndexStore: %v", err)
	}
	return store
}

func closeCrystallizationIndexTestStore(t *testing.T, store *CrystallizationIndexStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
