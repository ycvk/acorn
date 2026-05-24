package sqlite

import (
	"database/sql"
	"errors"
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
		{"session_messages", "content_parts", "TEXT NOT NULL DEFAULT ''", "v2_session_messages_content_parts"},
	}
	for _, m := range v2Migrations {
		if err := s.addColumnIfNotExists(m.table, m.column, m.def, m.version); err != nil {
			return err
		}
	}
	if err := s.dropLegacyMemoryTables(); err != nil {
		return err
	}
	if err := s.dropRemovedRuntimeTables(); err != nil {
		return err
	}
	if err := s.dropRemovedCodeintelTables(); err != nil {
		return err
	}
	return s.dropRemovedConversationFTS()
}

func (s *Store) dropLegacyMemoryTables() (err error) {
	const version = "v2_drop_legacy_memory_tables"
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&count); err == nil && count > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin legacy memory table drop: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("legacy memory table drop rollback: %w", rollbackErr))
		}
	}()

	statements := []string{
		`DROP TABLE IF EXISTS knowledge_search_idx`,
		`DROP TABLE IF EXISTS knowledge_search_docs`,
		`DROP TABLE IF EXISTS skill_patch_history`,
		`DROP TABLE IF EXISTS knowledge_facts`,
		`DROP TABLE IF EXISTS memory_evidence`,
		`DROP TABLE IF EXISTS episodic_memories`,
		`DROP TABLE IF EXISTS core_memory_blocks`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("drop legacy memory table: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record legacy memory table drop migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy memory table drop: %w", err)
	}
	return nil
}

func (s *Store) dropRemovedRuntimeTables() (err error) {
	const version = "v2_drop_removed_runtime_tables"
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&count); err == nil && count > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed runtime table drop: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("removed runtime table drop rollback: %w", rollbackErr))
		}
	}()

	if _, err := tx.Exec(`DROP TABLE IF EXISTS run_snapshots`); err != nil {
		return fmt.Errorf("drop removed runtime table: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record removed runtime table drop migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit removed runtime table drop: %w", err)
	}
	return nil
}

func (s *Store) dropRemovedCodeintelTables() (err error) {
	const version = "v2_drop_removed_codeintel_tables"
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&count); err == nil && count > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed codeintel table drop: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("removed codeintel table drop rollback: %w", rollbackErr))
		}
	}()

	statements := []string{
		`DROP TRIGGER IF EXISTS codeintel_search_docs_ai`,
		`DROP TRIGGER IF EXISTS codeintel_search_docs_ad`,
		`DROP TRIGGER IF EXISTS codeintel_search_docs_au`,
		`DROP TABLE IF EXISTS codeintel_search_idx`,
		`DROP TABLE IF EXISTS codeintel_search_docs`,
		`DROP TABLE IF EXISTS codeintel_edges`,
		`DROP TABLE IF EXISTS codeintel_symbols`,
		`DROP TABLE IF EXISTS codeintel_files`,
		`DROP TABLE IF EXISTS codeintel_index_meta`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("drop removed codeintel table: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record removed codeintel table drop migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit removed codeintel table drop: %w", err)
	}
	return nil
}

func (s *Store) dropRemovedConversationFTS() (err error) {
	const version = "v2_drop_removed_conversation_fts"
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&count); err == nil && count > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed conversation fts drop: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("removed conversation fts drop rollback: %w", rollbackErr))
		}
	}()

	statements := []string{
		`DROP TRIGGER IF EXISTS conv_seg_ai`,
		`DROP TRIGGER IF EXISTS conv_seg_ad`,
		`DROP TRIGGER IF EXISTS conv_seg_au`,
		`DROP TABLE IF EXISTS conversation_segments_idx`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("drop removed conversation fts object: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record removed conversation fts drop migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit removed conversation fts drop: %w", err)
	}
	return nil
}

func (s *Store) addColumnIfNotExists(table, column, definition, versionKey string) error {
	// Check schema_migrations for idempotency
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", versionKey)
	if err := row.Scan(&count); err == nil && count > 0 {
		return nil
	}

	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	if err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			// Column already exists (e.g., V2→V1→V2 scenario), record and continue
			if _, insertErr := s.db.Exec("INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", versionKey); insertErr != nil {
				return fmt.Errorf("record duplicate column migration %s: %w", versionKey, insertErr)
			}
			return nil
		}
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	_, insertErr := s.db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", versionKey)
	return insertErr
}

const storeBootstrapSchema = `
CREATE TABLE IF NOT EXISTS runs (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    turn_index INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    input_text TEXT NOT NULL,
    output_text TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    checkpoint_id TEXT NOT NULL DEFAULT '',
    orchestration_mode TEXT NOT NULL DEFAULT 'direct_response',
    parent_run_id TEXT NOT NULL DEFAULT '',
    depth INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS checkpoints (
    checkpoint_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    turn_index INTEGER NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    content_parts TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_messages_run_id ON session_messages(run_id);
CREATE TABLE IF NOT EXISTS pending_actions (
    action_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    interrupt_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    rule TEXT NOT NULL DEFAULT '',
    decision_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    decided_at TEXT NOT NULL DEFAULT '',
    resolved_at TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(run_id) REFERENCES runs(run_id)
);
CREATE INDEX IF NOT EXISTS idx_pending_actions_run_id_status_created_at ON pending_actions(run_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_actions_interrupt_id ON pending_actions(interrupt_id) WHERE interrupt_id <> '';
CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
    provider_name TEXT PRIMARY KEY,
    access_token TEXT,
    refresh_token TEXT,
    expiry TEXT,
    updated_at TEXT
);
CREATE TABLE IF NOT EXISTS owner_profile (
    owner_id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    platform TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT '',
    revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_devices_token_hash ON devices(token_hash);
CREATE TABLE IF NOT EXISTS device_push_tokens (
    push_token_id TEXT PRIMARY KEY,
    device_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    platform TEXT NOT NULL,
    token_value TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revoked_at TEXT NOT NULL DEFAULT '',
    UNIQUE(device_id, provider),
    FOREIGN KEY(device_id) REFERENCES devices(device_id)
);
CREATE INDEX IF NOT EXISTS idx_device_push_tokens_device_active ON device_push_tokens(device_id, revoked_at);
CREATE TABLE IF NOT EXISTS notifications (
    notification_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    action_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_kind_created_at ON notifications(kind, created_at DESC);
CREATE TABLE IF NOT EXISTS notification_deliveries (
    delivery_id TEXT PRIMARY KEY,
    notification_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    push_token_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(notification_id) REFERENCES notifications(notification_id),
    FOREIGN KEY(device_id) REFERENCES devices(device_id),
    FOREIGN KEY(push_token_id) REFERENCES device_push_tokens(push_token_id)
);
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_notification ON notification_deliveries(notification_id);
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_status ON notification_deliveries(status, updated_at DESC);
CREATE TABLE IF NOT EXISTS pairing_codes (
    code_hash TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL,
    used_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_segments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL UNIQUE,
    user_content TEXT NOT NULL DEFAULT '',
    assistant_content TEXT NOT NULL DEFAULT '',
    run_status TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_conv_seg_session_id ON conversation_segments(session_id);
CREATE INDEX IF NOT EXISTS idx_conv_seg_run_id ON conversation_segments(run_id);
CREATE TABLE IF NOT EXISTS working_checkpoints (
    session_id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    related_skill_id TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS run_context_snapshots (
    run_id TEXT PRIMARY KEY,
    working_checkpoint_content TEXT NOT NULL DEFAULT '',
    working_checkpoint_skill_id TEXT NOT NULL DEFAULT '',
    decision_profile_hash TEXT NOT NULL DEFAULT '',
    decision_action TEXT NOT NULL DEFAULT '',
    decision_skill_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS context_boundaries (
    boundary_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    turn_index INTEGER NOT NULL,
    mode TEXT NOT NULL,
    trigger TEXT NOT NULL,
    first_index INTEGER NOT NULL,
    last_index INTEGER NOT NULL,
    covered_first_message_id TEXT NOT NULL,
    covered_last_message_id TEXT NOT NULL,
    previous_boundary_id TEXT NOT NULL DEFAULT '',
    summary_message_id TEXT NOT NULL,
    transcript_ref TEXT NOT NULL,
    preserved_from_index INTEGER NOT NULL,
    preserved_to_index INTEGER NOT NULL,
    preserved_head_message_id TEXT NOT NULL,
    preserved_anchor_message_id TEXT NOT NULL,
    preserved_tail_message_id TEXT NOT NULL,
    tokens_before INTEGER NOT NULL,
    tokens_after INTEGER NOT NULL,
    effective_window_tokens INTEGER NOT NULL,
    summary TEXT NOT NULL,
    summary_snippet TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_context_boundaries_session_sequence ON context_boundaries(session_id, sequence DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_context_boundaries_run_sequence ON context_boundaries(run_id, sequence DESC, created_at DESC);
CREATE TABLE IF NOT EXISTS tool_results (
    result_ref TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    turn_index INTEGER NOT NULL DEFAULT 0,
    call_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    arguments_json TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error_reason TEXT NOT NULL DEFAULT '',
    preview TEXT NOT NULL,
    full_text TEXT NOT NULL,
    token_estimate INTEGER NOT NULL DEFAULT 0,
    side_effects_json TEXT NOT NULL,
    evidence_refs_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tool_results_run ON tool_results(run_id);
CREATE INDEX IF NOT EXISTS idx_tool_results_session ON tool_results(session_id);
CREATE TABLE IF NOT EXISTS artifacts (
    artifact_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    source_tool_result_ref TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    relative_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_artifacts_run ON artifacts(run_id, created_at ASC, artifact_id ASC);
CREATE INDEX IF NOT EXISTS idx_artifacts_session ON artifacts(session_id, created_at ASC, artifact_id ASC);
CREATE INDEX IF NOT EXISTS idx_artifacts_source_tool_result ON artifacts(source_tool_result_ref);
CREATE TABLE IF NOT EXISTS terminal_sessions (
    terminal_session_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    label TEXT NOT NULL DEFAULT '',
    command_json TEXT NOT NULL,
    cwd TEXT NOT NULL,
    interactive INTEGER NOT NULL DEFAULT 0,
    pty INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    pid INTEGER,
    process_group_id INTEGER,
    exit_code INTEGER,
    signal TEXT NOT NULL DEFAULT '',
    stdout_artifact_id TEXT NOT NULL DEFAULT '',
    stderr_artifact_id TEXT NOT NULL DEFAULT '',
    pty_artifact_id TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    ended_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_run ON terminal_sessions(run_id, created_at ASC, terminal_session_id ASC);
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_session ON terminal_sessions(session_id, created_at ASC, terminal_session_id ASC);
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_status ON terminal_sessions(status, updated_at DESC);
CREATE TABLE IF NOT EXISTS terminal_session_logs (
    log_id TEXT PRIMARY KEY,
    terminal_session_id TEXT NOT NULL,
    stream TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    start_offset INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_terminal_session_logs_session ON terminal_session_logs(terminal_session_id, stream, created_at ASC, log_id ASC);
CREATE TABLE IF NOT EXISTS provider_usages (
    usage_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    call_site TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    model_name TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_provider_usages_run ON provider_usages(run_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_provider_usages_session ON provider_usages(session_id, created_at ASC);
CREATE TABLE IF NOT EXISTS run_decisions (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    intent TEXT NOT NULL DEFAULT '',
    selected_skill_id TEXT NOT NULL DEFAULT '',
    decision_reason TEXT NOT NULL DEFAULT '',
    decision_profile_hash TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_decisions_session_created_at ON run_decisions(session_id, created_at DESC);
CREATE TABLE IF NOT EXISTS run_archives (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    input_excerpt TEXT NOT NULL DEFAULT '',
    output_excerpt TEXT NOT NULL DEFAULT '',
    touched_paths_json TEXT NOT NULL DEFAULT '[]',
    tool_names_json TEXT NOT NULL DEFAULT '[]',
    run_status TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_archives_session_created_at ON run_archives(session_id, created_at DESC);
CREATE TABLE IF NOT EXISTS session_summaries (
    session_id TEXT PRIMARY KEY,
    source_run_id TEXT NOT NULL DEFAULT '',
    run_status TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS plan_steps (
    plan_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    steps_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_steps_session ON plan_steps(session_id);
`

func (s *Store) validateSchema() error {
	requiredTables := map[string][]string{
		"runs":                    {"run_id", "session_id", "turn_index", "status", "input_text", "output_text", "error_text", "checkpoint_id", "orchestration_mode", "created_at", "updated_at", "parent_run_id", "depth"},
		"events":                  {"sequence", "run_id", "kind", "payload_json", "created_at"},
		"checkpoints":             {"checkpoint_id", "run_id", "payload", "created_at", "updated_at"},
		"sessions":                {"session_id", "title", "created_at", "updated_at"},
		"session_messages":        {"id", "session_id", "turn_index", "role", "content", "content_parts", "run_id", "created_at"},
		"pending_actions":         {"action_id", "run_id", "interrupt_id", "kind", "subject", "payload_json", "status", "mode", "reason", "rule", "decision_json", "created_at", "decided_at", "resolved_at"},
		"mcp_oauth_tokens":        {"provider_name", "access_token", "refresh_token", "expiry", "updated_at"},
		"owner_profile":           {"owner_id", "created_at"},
		"devices":                 {"device_id", "name", "platform", "token_hash", "created_at", "last_seen_at", "revoked_at"},
		"device_push_tokens":      {"push_token_id", "device_id", "provider", "platform", "token_value", "token_hash", "created_at", "updated_at", "revoked_at"},
		"notifications":           {"notification_id", "kind", "run_id", "action_id", "created_at"},
		"notification_deliveries": {"delivery_id", "notification_id", "device_id", "push_token_id", "provider", "status", "error", "created_at", "updated_at"},
		"pairing_codes":           {"code_hash", "expires_at", "used_at", "created_at"},
		"conversation_segments":   {"id", "session_id", "run_id", "user_content", "assistant_content", "run_status", "created_at"},
		"working_checkpoints":     {"session_id", "content", "related_skill_id", "updated_at"},
		"run_context_snapshots":   {"run_id", "working_checkpoint_content", "working_checkpoint_skill_id", "decision_profile_hash", "decision_action", "decision_skill_id", "created_at"},
		"context_boundaries":      {"boundary_id", "session_id", "run_id", "sequence", "turn_index", "mode", "trigger", "first_index", "last_index", "covered_first_message_id", "covered_last_message_id", "previous_boundary_id", "summary_message_id", "transcript_ref", "preserved_from_index", "preserved_to_index", "preserved_head_message_id", "preserved_anchor_message_id", "preserved_tail_message_id", "tokens_before", "tokens_after", "effective_window_tokens", "summary", "summary_snippet", "created_at"},
		"tool_results":            {"result_ref", "run_id", "session_id", "turn_index", "call_id", "tool_name", "arguments_json", "status", "error_reason", "preview", "full_text", "token_estimate", "side_effects_json", "evidence_refs_json", "created_at"},
		"artifacts":               {"artifact_id", "run_id", "session_id", "source_tool_result_ref", "kind", "title", "mime_type", "relative_path", "size_bytes", "sha256", "created_at"},
		"terminal_sessions":       {"terminal_session_id", "run_id", "session_id", "label", "command_json", "cwd", "interactive", "pty", "status", "pid", "process_group_id", "exit_code", "signal", "stdout_artifact_id", "stderr_artifact_id", "pty_artifact_id", "started_at", "ended_at", "created_at", "updated_at"},
		"terminal_session_logs":   {"log_id", "terminal_session_id", "stream", "artifact_id", "start_offset", "size_bytes", "created_at"},
		"provider_usages":         {"usage_id", "run_id", "session_id", "call_site", "provider_name", "model_name", "prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "reasoning_tokens", "created_at"},
		"run_decisions":           {"run_id", "session_id", "action", "intent", "selected_skill_id", "decision_reason", "decision_profile_hash", "created_at"},
		"run_archives":            {"run_id", "session_id", "input_excerpt", "output_excerpt", "touched_paths_json", "tool_names_json", "run_status", "created_at"},
		"session_summaries":       {"session_id", "source_run_id", "run_status", "summary", "updated_at"},
		"schema_migrations":       {"version", "applied_at"},
		"plan_steps":              {"plan_id", "session_id", "run_id", "steps_json", "created_at", "updated_at"},
	}
	for table, columns := range requiredTables {
		if err := s.requireColumns(table, columns); err != nil {
			return err
		}
	}
	return nil
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
