package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) createSchema() error {
	if _, err := s.db.Exec(storeBootstrapTables); err != nil {
		return fmt.Errorf("create sqlite schema (tables): %w", err)
	}
	if err := s.validateSchema(); err != nil {
		return err
	}
	if err := s.createMemorySchema(); err != nil {
		return err
	}
	if _, err := s.db.Exec(storeBootstrapIndexes); err != nil {
		return fmt.Errorf("create sqlite schema (indexes): %w", err)
	}
	return nil
}

func (s *Store) validateSchema() error {
	for table, columns := range schemaRequiredTables {
		if err := s.requireColumns(table, columns); err != nil {
			return err
		}
	}
	return nil
}

// schemaRequiredTables maps each required table to the columns that must exist
// after migration; validateSchema enforces presence to detect a stale or
// incompatible local database.
var schemaRequiredTables = map[string][]string{
	"runs":                {"run_id", "session_id", "turn_index", "status", "input_text", "output_text", "error_text", "created_at", "finished_at"},
	"events":              {"sequence", "run_id", "kind", "payload_json", "created_at"},
	"sessions":            {"session_id", "title", "created_at", "updated_at"},
	"session_messages":    {"id", "session_id", "turn_index", "role", "content", "run_id", "created_at"},
	"pending_actions":     {"action_id", "run_id", "interrupt_id", "kind", "subject", "payload_json", "status", "reason", "decision_json", "created_at", "resolved_at"},
	"mcp_oauth_tokens":    {"provider_name", "access_token", "refresh_token", "expiry", "updated_at"},
	"devices":             {"device_id", "name", "platform", "token_hash", "created_at", "last_seen_at", "revoked_at"},
	"pairing_codes":       {"code_hash", "expires_at", "used_at", "created_at"},
	"artifacts":           {"artifact_id", "run_id", "session_id", "source_tool_result_ref", "kind", "title", "mime_type", "relative_path", "size_bytes", "sha256", "created_at"},
	"agent_checkpoints":   {"checkpoint_id", "data", "updated_at"},
	"context_snapshots":   {"hash", "content", "created_at"},
	"push_tokens":         {"device_id", "token", "updated_at"},
	"notifications":       {"id", "title", "body", "thread_id", "run_id", "status", "send_after", "error_text", "created_at", "sent_at"},
	"knowledge_notes":     {"path", "title", "tags", "body", "mtime_ns", "size", "updated_at"},
	"watches":             {"id", "name", "kind", "target", "selector", "mode", "interval_seconds", "status", "session_id", "next_check_at", "last_checked_at", "last_error", "failures", "snapshot", "created_at", "updated_at"},
	"watch_items":         {"id", "watch_id", "item_key", "title", "url", "summary", "published_at", "status", "run_id", "seen_at"},
	"routine_runs":        {"routine", "slot", "thread_id", "run_id", "created_at"},
	"phone_notifications": {"id", "device_id", "notification_key", "package", "app", "title", "text", "posted_at", "received_at"},
}

func (s *Store) requireColumns(table string, columns []string) error {
	existing, err := s.tableColumns(table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if _, ok := existing[column]; ok {
			continue
		}
		return fmt.Errorf("%w: table %s is missing required column %s; rebuild the local database with a clean current storage directory", core.ErrUnsupportedStorageSchema, table, column)
	}
	return nil
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

func (s *Store) configure() error {
	if _, err := s.db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA busy_timeout = 5000;`, err)
	}
	mode, err := s.journalMode()
	if err != nil {
		return err
	}
	if !strings.EqualFold(mode, "wal") {
		row := s.db.QueryRow(`PRAGMA journal_mode = WAL;`)
		var applied string
		if err := row.Scan(&applied); err != nil {
			return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA journal_mode = WAL;`, err)
		}
		if !strings.EqualFold(strings.TrimSpace(applied), "wal") {
			return fmt.Errorf("configure sqlite pragma %q: applied mode %q", `PRAGMA journal_mode = WAL;`, applied)
		}
	}
	if _, err := s.db.Exec(`PRAGMA synchronous = NORMAL;`); err != nil {
		return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA synchronous = NORMAL;`, err)
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA foreign_keys = ON;`, err)
	}
	return nil
}

func (s *Store) journalMode() (string, error) {
	row := s.db.QueryRow(`PRAGMA journal_mode;`)
	var mode string
	if err := row.Scan(&mode); err != nil {
		return "", fmt.Errorf("query sqlite pragma %q: %w", `PRAGMA journal_mode;`, err)
	}
	return strings.TrimSpace(mode), nil
}

// rollbackOnErr rolls back a transaction if the surrounding function returned
// an error, swallowing the benign ErrTxDone that fires after a successful Commit.
func rollbackOnErr(tx *sql.Tx, err *error, label string) {
	rollbackErr := tx.Rollback()
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		*err = errors.Join(*err, fmt.Errorf("%s rollback: %w", label, rollbackErr))
	}
}
