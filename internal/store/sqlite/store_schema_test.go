package sqlite

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/store"
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
	if !errors.Is(err, store.ErrUnsupportedStorageSchema) {
		t.Fatalf("Open error = %v, want store.ErrUnsupportedStorageSchema", err)
	}
	if !strings.Contains(err.Error(), "runs") || !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("unexpected error detail: %v", err)
	}
}

func TestStoreSchemaIncludesV2Tables(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for table, column := range map[string]string{
		"working_checkpoints":   "related_skill_id",
		"run_context_snapshots": "decision_skill_id",
		"context_boundaries":    "boundary_id",
		"run_decisions":         "decision_reason",
		"run_archives":          "tool_names_json",
		"session_summaries":     "source_run_id",
		"provider_usages":       "cached_tokens",
		"artifacts":             "source_tool_result_ref",
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

func TestV2MigrationAddsNewColumns(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	v2Checks := []struct{ table, column string }{
		{"runs", "parent_run_id"},
		{"runs", "depth"},
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

func TestMigrationDropsLegacyMemoryTables(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE core_memory_blocks(id TEXT PRIMARY KEY);
CREATE TABLE episodic_memories(id TEXT PRIMARY KEY);
CREATE TABLE memory_evidence(id TEXT PRIMARY KEY);
CREATE TABLE knowledge_facts(id INTEGER PRIMARY KEY AUTOINCREMENT);
CREATE TABLE knowledge_search_docs(id INTEGER PRIMARY KEY AUTOINCREMENT);
CREATE VIRTUAL TABLE knowledge_search_idx USING fts5(search_text);
CREATE TABLE skill_patch_history(id INTEGER PRIMARY KEY AUTOINCREMENT);`); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy memory tables: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, table := range []string{
		"core_memory_blocks",
		"episodic_memories",
		"memory_evidence",
		"knowledge_facts",
		"knowledge_search_docs",
		"knowledge_search_idx",
		"skill_patch_history",
	} {
		if tableExists(t, store, table) {
			t.Fatalf("legacy memory table %s still exists after migration", table)
		}
	}
}

func TestMigrationDropsRemovedTerminalSessionTables(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE terminal_sessions(terminal_session_id TEXT PRIMARY KEY);
CREATE TABLE terminal_session_logs(log_id TEXT PRIMARY KEY, terminal_session_id TEXT NOT NULL);`); err != nil {
		_ = db.Close()
		t.Fatalf("seed removed terminal session tables: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, table := range []string{"terminal_sessions", "terminal_session_logs"} {
		if tableExists(t, store, table) {
			t.Fatalf("removed terminal session table %s still exists after migration", table)
		}
	}
}

func TestMigrationDropsRemovedCodeintelTables(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE codeintel_files(path TEXT PRIMARY KEY);
CREATE TABLE codeintel_symbols(id INTEGER PRIMARY KEY AUTOINCREMENT);
CREATE TABLE codeintel_edges(source_path TEXT NOT NULL);
CREATE TABLE codeintel_search_docs(id INTEGER PRIMARY KEY AUTOINCREMENT, search_text TEXT);
CREATE VIRTUAL TABLE codeintel_search_idx USING fts5(search_text);
CREATE TRIGGER codeintel_search_docs_ai AFTER INSERT ON codeintel_search_docs BEGIN
    INSERT INTO codeintel_search_idx(rowid, search_text) VALUES (new.id, new.search_text);
END;
CREATE TABLE codeintel_index_meta(root_path TEXT PRIMARY KEY);`); err != nil {
		_ = db.Close()
		t.Fatalf("seed removed codeintel tables: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, table := range []string{
		"codeintel_files",
		"codeintel_symbols",
		"codeintel_edges",
		"codeintel_search_docs",
		"codeintel_search_idx",
		"codeintel_index_meta",
	} {
		if tableExists(t, store, table) {
			t.Fatalf("removed codeintel table %s still exists after migration", table)
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

func TestMigrationDropsRemovedConversationFTS(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(storeBootstrapSchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed current schema: %v", err)
	}
	if _, err := db.Exec(`
CREATE VIRTUAL TABLE conversation_segments_idx
    USING fts5(user_content, assistant_content,
               content='conversation_segments',
               content_rowid='id',
               tokenize='trigram');
CREATE TRIGGER conv_seg_ai AFTER INSERT ON conversation_segments BEGIN
    INSERT INTO conversation_segments_idx(rowid, user_content, assistant_content)
        VALUES (new.id, new.user_content, new.assistant_content);
END;
CREATE TRIGGER conv_seg_ad AFTER DELETE ON conversation_segments BEGIN
    INSERT INTO conversation_segments_idx(conversation_segments_idx, rowid, user_content, assistant_content)
        VALUES('delete', old.id, old.user_content, old.assistant_content);
END;
CREATE TRIGGER conv_seg_au AFTER UPDATE ON conversation_segments BEGIN
    INSERT INTO conversation_segments_idx(conversation_segments_idx, rowid, user_content, assistant_content)
        VALUES('delete', old.id, old.user_content, old.assistant_content);
    INSERT INTO conversation_segments_idx(rowid, user_content, assistant_content)
        VALUES (new.id, new.user_content, new.assistant_content);
END;`); err != nil {
		_ = db.Close()
		t.Fatalf("seed removed conversation fts: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if tableExists(t, store, "conversation_segments_idx") {
		t.Fatal("conversation_segments_idx still exists after migration")
	}
	for _, trigger := range []string{"conv_seg_ai", "conv_seg_ad", "conv_seg_au"} {
		if triggerExists(t, store, trigger) {
			t.Fatalf("trigger %s still exists after migration", trigger)
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

func triggerExists(t *testing.T, store *Store, trigger string) bool {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(1) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&count); err != nil {
		t.Fatalf("check trigger %s existence: %v", trigger, err)
	}
	return count > 0
}
