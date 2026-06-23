package store

import (
	"fmt"
	"strings"

	"database/sql"
)

func (s *Store) migrate() error {
	if _, err := s.db.Exec(storeBootstrapSchema); err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	if err := s.migrateV2(); err != nil {
		return fmt.Errorf("migrate v2: %w", err)
	}
	if err := s.ensureOwnerProfile(); err != nil {
		return fmt.Errorf("ensure owner profile: %w", err)
	}
	if err := s.validateSchema(); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureOwnerProfile() error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO owner_profile(owner_id, created_at) VALUES(?, ?)`,
		"owner",
		formatTimestamp(timeNowUTC()),
	)
	return err
}

func (s *Store) migrateV2() error {
	// The only remaining v2 column addition is session_messages.content_parts;
	// the runs columns (parent_run_id, depth, orchestration_mode, skill_id)
	// were retired by the architecture refactor and are no longer created.
	if err := s.addColumnIfNotExists("session_messages", "content_parts", "TEXT NOT NULL DEFAULT ''", "v2_session_messages_content_parts"); err != nil {
		return err
	}
	if err := s.dropLegacyTablesChain(); err != nil {
		return err
	}
	return s.dropDecisionTables()
}

// dropLegacyTablesChain runs the sequence of legacy/removed table drops that
// follow the v2 column additions, in migration order. The final step drops
// the tables retired by the architecture refactor (plan_steps, checkpoints,
// tool_results, conversation_segments, run_archives, working_checkpoints,
// provider_usages, run_context_snapshots, context_boundaries).
func (s *Store) dropLegacyTablesChain() error {
	drops := []func() error{
		s.dropLegacyMemoryTables,
		s.dropRemovedRuntimeTables,
		s.dropRemovedTerminalSessionTables,
		s.dropRemovedCodeintelTables,
		s.dropRemovedConversationFTS,
		s.dropRefactoredTables,
	}
	for _, drop := range drops {
		if err := drop(); err != nil {
			return err
		}
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
// incompatible local database. Retired tables (checkpoints, plan_steps,
// conversation_segments, working_checkpoints, run_context_snapshots,
// context_boundaries, tool_results, provider_usages, run_archives) are dropped
// by migrations and intentionally absent.
var schemaRequiredTables = map[string][]string{
	"runs":              {"run_id", "session_id", "turn_index", "status", "input_text", "output_text", "error_text", "created_at", "updated_at"},
	"events":            {"sequence", "run_id", "kind", "payload_json", "created_at"},
	"sessions":          {"session_id", "title", "created_at", "updated_at"},
	"session_messages":  {"id", "session_id", "turn_index", "role", "content", "content_parts", "run_id", "created_at"},
	"pending_actions":   {"action_id", "run_id", "interrupt_id", "kind", "subject", "payload_json", "status", "reason", "decision_json", "created_at", "decided_at", "resolved_at"},
	"mcp_oauth_tokens":  {"provider_name", "access_token", "refresh_token", "expiry", "updated_at"},
	"owner_profile":     {"owner_id", "created_at"},
	"devices":           {"device_id", "name", "platform", "token_hash", "created_at", "last_seen_at", "revoked_at"},
	"pairing_codes":     {"code_hash", "expires_at", "used_at", "created_at"},
	"artifacts":         {"artifact_id", "run_id", "session_id", "source_tool_result_ref", "kind", "title", "mime_type", "relative_path", "size_bytes", "sha256", "created_at"},
	"session_summaries": {"session_id", "source_run_id", "run_status", "summary", "updated_at"},
	"schema_migrations": {"version", "applied_at"},
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
		return fmt.Errorf("%w: table %s is missing required column %s; rebuild the local database with a clean current storage directory", ErrUnsupportedStorageSchema, table, column)
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

// addColumnIfNotExists adds a column to a table idempotently, recording the
// migration version in schema_migrations so it is not re-run. A column that
// already exists (e.g. V2→V1→V2) is recorded as a successful no-op.
func (s *Store) addColumnIfNotExists(table, column, definition, versionKey string) error {
	// Check schema_migrations for idempotency
	if migrationApplied(s.db, versionKey) {
		return nil
	}

	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	if err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("add column %s.%s: %w", table, column, err)
		}
		// Column already exists (e.g., V2→V1→V2 scenario), record and continue
		if _, insertErr := s.db.Exec("INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", versionKey); insertErr != nil {
			return fmt.Errorf("record duplicate column migration %s: %w", versionKey, insertErr)
		}
		return nil
	}
	_, insertErr := s.db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", versionKey)
	return insertErr
}
