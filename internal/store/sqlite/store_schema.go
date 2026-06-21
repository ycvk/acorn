package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/store"
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
	v2Migrations := []struct {
		table   string
		column  string
		def     string
		version string
	}{
		{"runs", "parent_run_id", "TEXT NOT NULL DEFAULT ''", "v2_runs_parent_run_id"},
		{"runs", "depth", "INTEGER NOT NULL DEFAULT 0", "v2_runs_depth"},
		{"runs", "orchestration_mode", "TEXT NOT NULL DEFAULT 'direct_response'", "v2_runs_orchestration_mode"},
		{"runs", "skill_id", "TEXT NOT NULL DEFAULT ''", "v2_runs_skill_id"},
		{"session_messages", "content_parts", "TEXT NOT NULL DEFAULT ''", "v2_session_messages_content_parts"},
	}
	for _, m := range v2Migrations {
		if err := s.addColumnIfNotExists(m.table, m.column, m.def, m.version); err != nil {
			return err
		}
	}
	if err := s.dropLegacyTablesChain(); err != nil {
		return err
	}
	return s.dropDecisionTables()
}

// dropLegacyTablesChain runs the sequence of legacy/removed table drops that
// follow the v2 column additions, in migration order.
func (s *Store) dropLegacyTablesChain() error {
	drops := []func() error{
		s.dropLegacyMemoryTables,
		s.dropRemovedRuntimeTables,
		s.dropRemovedTerminalSessionTables,
		s.dropRemovedCodeintelTables,
		s.dropRemovedConversationFTS,
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
// incompatible local database.
var schemaRequiredTables = map[string][]string{
	"runs":                  {"run_id", "session_id", "turn_index", "status", "input_text", "output_text", "error_text", "checkpoint_id", "orchestration_mode", "skill_id", "created_at", "updated_at", "parent_run_id", "depth"},
	"events":                {"sequence", "run_id", "kind", "payload_json", "created_at"},
	"checkpoints":           {"checkpoint_id", "run_id", "payload", "created_at", "updated_at"},
	"sessions":              {"session_id", "title", "created_at", "updated_at"},
	"session_messages":      {"id", "session_id", "turn_index", "role", "content", "content_parts", "run_id", "created_at"},
	"pending_actions":       {"action_id", "run_id", "interrupt_id", "kind", "subject", "payload_json", "status", "mode", "reason", "rule", "decision_json", "created_at", "decided_at", "resolved_at"},
	"mcp_oauth_tokens":      {"provider_name", "access_token", "refresh_token", "expiry", "updated_at"},
	"owner_profile":         {"owner_id", "created_at"},
	"devices":               {"device_id", "name", "platform", "token_hash", "created_at", "last_seen_at", "revoked_at"},
	"pairing_codes":         {"code_hash", "expires_at", "used_at", "created_at"},
	"conversation_segments": {"id", "session_id", "run_id", "user_content", "assistant_content", "run_status", "created_at"},
	"working_checkpoints":   {"session_id", "content", "related_skill_id", "updated_at"},
	"run_context_snapshots": {"run_id", "working_checkpoint_content", "working_checkpoint_skill_id", "created_at"},
	"context_boundaries":    {"boundary_id", "session_id", "run_id", "sequence", "turn_index", "mode", "trigger", "first_index", "last_index", "covered_first_message_id", "covered_last_message_id", "previous_boundary_id", "summary_message_id", "transcript_ref", "preserved_from_index", "preserved_to_index", "preserved_head_message_id", "preserved_anchor_message_id", "preserved_tail_message_id", "tokens_before", "tokens_after", "effective_window_tokens", "summary", "summary_snippet", "created_at"},
	"tool_results":          {"result_ref", "run_id", "session_id", "turn_index", "call_id", "tool_name", "arguments_json", "status", "error_reason", "preview", "full_text", "token_estimate", "side_effects_json", "evidence_refs_json", "created_at"},
	"artifacts":             {"artifact_id", "run_id", "session_id", "source_tool_result_ref", "kind", "title", "mime_type", "relative_path", "size_bytes", "sha256", "created_at"},
	"provider_usages":       {"usage_id", "run_id", "session_id", "call_site", "provider_name", "model_name", "prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "reasoning_tokens", "created_at"},
	"run_archives":          {"run_id", "session_id", "input_excerpt", "output_excerpt", "touched_paths_json", "tool_names_json", "run_status", "created_at"},
	"session_summaries":     {"session_id", "source_run_id", "run_status", "summary", "updated_at"},
	"schema_migrations":     {"version", "applied_at"},
	"plan_steps":            {"plan_id", "session_id", "run_id", "steps_json", "created_at", "updated_at"},
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
		return fmt.Errorf("%w: table %s is missing required column %s; rebuild the local database with a clean current storage directory", store.ErrUnsupportedStorageSchema, table, column)
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
