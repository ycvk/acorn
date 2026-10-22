package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// createSchema lays out an empty database at schemaVersion in one
// transaction.
func createSchema(ctx context.Context, db *sql.DB) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackOnErr(tx, &err, "create sqlite schema")
	for _, step := range []struct{ name, ddl string }{
		{"tables", storeBootstrapTables},
		{"memory tables", memorySchema},
		{"memory indexes", memoryQueryIndexes},
		{"memory triggers", memorySourceTriggers},
		{"indexes", storeBootstrapIndexes},
	} {
		if _, err := tx.ExecContext(ctx, step.ddl); err != nil {
			return fmt.Errorf("create sqlite schema (%s): %w", step.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return tx.Commit()
}

func (s *Store) validateSchema() error {
	for _, required := range []map[string][]string{schemaRequiredTables, memoryRequiredTables} {
		for table, columns := range required {
			if err := s.requireColumns(table, columns); err != nil {
				return err
			}
		}
	}
	return nil
}

// schemaRequiredTables maps each required table to the columns that must exist
// after migration; validateSchema enforces presence to detect a stale or
// incompatible local database.
var schemaRequiredTables = map[string][]string{
	"runs":                      {"run_id", "session_id", "turn_index", "status", "input_text", "output_text", "error_text", "created_at", "finished_at"},
	"events":                    {"sequence", "run_id", "kind", "payload_json", "created_at"},
	"sessions":                  {"session_id", "title", "created_at", "updated_at"},
	"session_messages":          {"id", "session_id", "turn_index", "role", "content", "run_id", "created_at"},
	"pending_actions":           {"action_id", "run_id", "interrupt_id", "kind", "subject", "payload_json", "status", "reason", "decision_json", "created_at", "resolved_at"},
	"mcp_oauth_tokens":          {"provider_name", "access_token", "refresh_token", "expiry", "updated_at"},
	"devices":                   {"device_id", "name", "platform", "token_hash", "created_at", "last_seen_at", "revoked_at"},
	"pairing_codes":             {"code_hash", "expires_at", "used_at", "created_at"},
	"artifacts":                 {"artifact_id", "run_id", "session_id", "source_tool_result_ref", "kind", "title", "mime_type", "relative_path", "size_bytes", "sha256", "created_at"},
	"agent_checkpoints":         {"checkpoint_id", "data", "updated_at"},
	"context_snapshots":         {"hash", "content", "created_at"},
	"push_installations":        {"device_id", "fid", "updated_at"},
	"notifications":             {"id", "title", "body", "thread_id", "run_id", "status", "send_after", "error_text", "created_at", "sent_at", "action_id", "browser_url"},
	"knowledge_notes":           {"path", "title", "tags", "source", "body", "revision", "created_at", "updated_at"},
	"knowledge_revisions":       {"path", "revision", "title", "tags", "source", "body", "body_sha256", "run_id", "at"},
	"skills":                    {"id", "markdown", "revision", "retired", "seed_sha256", "created_at", "updated_at"},
	"skill_revisions":           {"id", "revision", "markdown", "markdown_sha256", "retired", "author", "run_id", "at"},
	"skill_files":               {"id", "revision", "path", "content", "size", "sha256"},
	"skill_package_stages":      {"sha256", "name", "markdown", "staged_at"},
	"skill_package_stage_files": {"sha256", "path", "content"},
	"skill_secrets":             {"skill", "name", "value", "updated_at"},
	"skill_triggers":            {"skill", "name", "schedule", "status", "next_at", "failures", "last_error", "last_run_at"},
	"skill_trigger_events":      {"id", "skill", "trigger_name", "event_key", "text", "status", "run_id", "seen_at"},
	"skill_threads":             {"skill", "thread_id"},
	"persona_revisions":         {"revision", "content", "content_sha256", "author", "device_id", "at"},
	"owner":                     {"id", "name", "updated_at"},
	"watches":                   {"id", "name", "kind", "target", "selector", "mode", "section", "issue", "interval_seconds", "status", "session_id", "next_check_at", "last_checked_at", "last_error", "failures", "snapshot", "created_at", "updated_at"},
	"news_sections":             {"name", "position", "preference", "created_at", "updated_at"},
	"watch_items":               {"id", "watch_id", "item_key", "title", "url", "summary", "published_at", "status", "run_id", "seen_at"},
	"routine_runs":              {"routine", "slot", "thread_id", "run_id", "created_at"},
	"phone_notifications":       {"id", "device_id", "notification_key", "package", "app", "title", "text", "posted_at", "received_at"},
	"thread_files":              {"file_id", "session_id", "message_id", "run_id", "origin", "name", "mime_type", "size_bytes", "sha256", "created_at"},
	"thread_views":              {"view_id", "session_id", "run_id", "title", "catalog", "components_json", "data_json", "source", "created_at", "updated_at", "pinned_at"},
	"thread_view_actions":       {"view_id", "action_key", "session_id", "component_id", "scope_json", "name", "selected_json", "context_json", "message_id", "created_at"},
	"run_processes":             {"run_id", "process"},
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
