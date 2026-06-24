package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !errors.Is(err, ErrUnsupportedStorageSchema) {
		t.Fatalf("Open error = %v, want ErrUnsupportedStorageSchema", err)
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
		"runs":             "finished_at",
		"events":           "payload_json",
		"sessions":         "title",
		"session_messages": "content_parts",
		"pending_actions":  "decision_json",
		"artifacts":        "source_tool_result_ref",
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
	// conversation_segments is no longer in the bootstrap schema; create it
	// manually so the legacy FTS triggers can attach before migration drops it.
	if _, err := db.Exec(`CREATE TABLE conversation_segments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL UNIQUE,
    user_content TEXT NOT NULL DEFAULT '',
    assistant_content TEXT NOT NULL DEFAULT '',
    run_status TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		t.Fatalf("seed conversation_segments: %v", err)
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

func TestMigrationDropsDecisionTables(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	// Seed a pre-migration schema that still has run_decisions and decision_* columns.
	if _, err := db.Exec(storeBootstrapSchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed current schema: %v", err)
	}
	// run_context_snapshots is no longer in the bootstrap schema; create it
	// manually so the legacy decision_* columns can be added before migration.
	if _, err := db.Exec(`CREATE TABLE run_context_snapshots (
    run_id TEXT PRIMARY KEY,
    working_checkpoint_content TEXT NOT NULL DEFAULT '',
    working_checkpoint_skill_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		t.Fatalf("seed run_context_snapshots: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE run_context_snapshots ADD COLUMN decision_profile_hash TEXT NOT NULL DEFAULT ''`); err != nil {
		_ = db.Close()
		t.Fatalf("add decision_profile_hash: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE run_context_snapshots ADD COLUMN decision_action TEXT NOT NULL DEFAULT ''`); err != nil {
		_ = db.Close()
		t.Fatalf("add decision_action: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE run_context_snapshots ADD COLUMN decision_skill_id TEXT NOT NULL DEFAULT ''`); err != nil {
		_ = db.Close()
		t.Fatalf("add decision_skill_id: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if tableExists(t, store, "run_decisions") {
		t.Fatal("run_decisions table still exists after migration")
	}
	// run_context_snapshots is dropped entirely by the refactored table drop.
	if tableExists(t, store, "run_context_snapshots") {
		t.Fatal("run_context_snapshots table still exists after migration")
	}
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
