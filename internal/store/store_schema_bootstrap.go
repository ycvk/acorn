package store

// storeBootstrapTables creates the 10 core tables if they do not already
// exist. This is split from index creation so that validateSchema can detect
// a stale/incompatible legacy database (missing columns) before index
// creation attempts to reference those columns.
//
// Tables per spec §5.6: sessions, session_messages, runs, events,
// pending_actions, devices, pairing_codes, artifacts, mcp_oauth_tokens,
// schema_migrations. owner_profile and session_summaries are deleted.
const storeBootstrapTables = `
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

CREATE TABLE IF NOT EXISTS runs (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    turn_index INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    input_text TEXT NOT NULL,
    output_text TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pending_actions (
    action_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    interrupt_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    decision_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT ''
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

CREATE TABLE IF NOT EXISTS pairing_codes (
    code_hash TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL,
    used_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

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

CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
    provider_name TEXT PRIMARY KEY,
    access_token TEXT,
    refresh_token TEXT,
    expiry TEXT,
    updated_at TEXT
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);
`

// storeBootstrapIndexes creates all indexes after table creation and schema
// validation have succeeded, ensuring columns referenced by indexes exist.
const storeBootstrapIndexes = `
CREATE INDEX IF NOT EXISTS idx_session_messages_run_id ON session_messages(run_id);
CREATE INDEX IF NOT EXISTS idx_session_messages_session_turn ON session_messages(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_runs_session_created ON runs(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_run_sequence ON events(run_id, sequence ASC);
CREATE INDEX IF NOT EXISTS idx_pending_actions_run_id_status ON pending_actions(run_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_actions_interrupt_id ON pending_actions(interrupt_id) WHERE interrupt_id <> '';
CREATE INDEX IF NOT EXISTS idx_devices_token_hash ON devices(token_hash);
CREATE INDEX IF NOT EXISTS idx_artifacts_run ON artifacts(run_id, created_at ASC, artifact_id ASC);
CREATE INDEX IF NOT EXISTS idx_artifacts_session ON artifacts(session_id, created_at ASC, artifact_id ASC);
`

// storeBootstrapSchema is the full bootstrap DDL (tables + indexes) kept for
// backward compatibility with tests that seed a pre-migration database.
const storeBootstrapSchema = storeBootstrapTables + storeBootstrapIndexes
