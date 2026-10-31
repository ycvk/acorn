package store

import (
	"database/sql"
	"fmt"
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
		"session_messages": "run_id",
		"pending_actions":  "decision_json",
		"artifacts":        "source_tool_result_ref",
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
func TestFreshSchemaHasNoSessionMessageContentParts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	columns, err := store.tableColumns("session_messages")
	if err != nil {
		t.Fatalf("table info session_messages: %v", err)
	}
	if _, ok := columns["content_parts"]; ok {
		t.Fatal("session_messages should not have content_parts")
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

func TestReopenKeepsSchema(t *testing.T) {
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
		t.Fatalf("reopen: %v", err)
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

func (s *Store) tableColumns(table string) (map[string]struct{}, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("table info %s: %w", table, err)
	}
	defer rows.Close()

	result := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan table info %s: %w", table, err)
		}
		result[name] = struct{}{}
	}
	return result, rows.Err()
}
