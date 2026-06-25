package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestOpenConfiguresAndKeepsWALMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	mode, err := store.journalMode()
	if err != nil {
		t.Fatalf("journalMode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode = %q, want wal", mode)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	mode, err = reopened.journalMode()
	if err != nil {
		t.Fatalf("journalMode after reopen: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode after reopen = %q, want wal", mode)
	}
}

func TestOpenRejectsLegacyRunsSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE runs (
    run_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    input_text TEXT NOT NULL,
    output_text TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    checkpoint_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	_, err = Open(dir)
	if !errors.Is(err, core.ErrUnsupportedStorageSchema) {
		t.Fatalf("Open error = %v, want core.ErrUnsupportedStorageSchema", err)
	}
	if !strings.Contains(err.Error(), "runs") {
		t.Fatalf("unexpected error detail: %v", err)
	}
}

func TestStoreSchemaIncludesCoreTables(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for table, column := range map[string]string{
		"runs":              "finished_at",
		"events":            "payload_json",
		"sessions":          "title",
		"session_messages":  "content_parts",
		"pending_actions":   "decision_json",
		"artifacts":         "source_tool_result_ref",
		"schema_migrations": "version",
	} {
		columns, err := store.tableColumns(table)
		if err != nil {
			t.Fatalf("table info %s: %v", table, err)
		}
		if _, ok := columns[column]; !ok {
			t.Fatalf("table %s missing expected column %s", table, column)
		}
	}
}

func TestEventsQueriesUseRunSequenceIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if !indexExists(t, store, "idx_events_run_sequence") {
		t.Fatal("events table missing idx_events_run_sequence")
	}
	plan := explainQueryPlan(t, store, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? AND sequence > ? ORDER BY sequence ASC`, "run_1", 10)
	if !strings.Contains(plan, "idx_events_run_sequence") {
		t.Fatalf("query plan = %q, want idx_events_run_sequence", plan)
	}
}
func TestV2MigrationAddsContentPartsColumn(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	v2Checks := []struct{ table, column string }{
		{"session_messages", "content_parts"},
		{"schema_migrations", "version"},
		{"schema_migrations", "applied_at"},
	}
	for _, check := range v2Checks {
		columns, err := store.tableColumns(check.table)
		if err != nil {
			t.Fatalf("table info %s: %v", check.table, err)
		}
		if _, ok := columns[check.column]; !ok {
			t.Errorf("table %s missing V2 column %s", check.table, check.column)
		}
	}
}

func TestSchemaDoesNotCreateConversationFTS(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if tableExists(t, store, "conversation_segments_idx") {
		t.Fatal("conversation_segments_idx should not exist")
	}
	for _, trigger := range []string{"conv_seg_ai", "conv_seg_ad", "conv_seg_au"} {
		if triggerExists(t, store, trigger) {
			t.Fatalf("trigger %s should not exist", trigger)
		}
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen (idempotent): %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func tableExists(t *testing.T, store *Store, table string) bool {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(1) FROM sqlite_master WHERE name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("check table %s existence: %v", table, err)
	}
	return count > 0
}

func indexExists(t *testing.T, store *Store, index string) bool {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
		t.Fatalf("check index %s existence: %v", index, err)
	}
	return count > 0
}

func explainQueryPlan(t *testing.T, store *Store, query string, args ...any) string {
	t.Helper()
	rows, err := store.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan query plan: %v", err)
		}
		parts = append(parts, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query plan rows: %v", err)
	}
	return strings.Join(parts, "\n")
}

func triggerExists(t *testing.T, store *Store, trigger string) bool {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(1) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&count); err != nil {
		t.Fatalf("check trigger %s existence: %v", trigger, err)
	}
	return count > 0
}
