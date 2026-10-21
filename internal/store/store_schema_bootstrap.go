package store

// storeBootstrapTables creates the 20 core tables if they do not already
// exist. This is split from index creation so that validateSchema can detect
// a stale/incompatible database (missing columns) before index creation
// attempts to reference those columns.
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

CREATE TABLE IF NOT EXISTS agent_checkpoints (
    checkpoint_id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
    provider_name TEXT PRIMARY KEY,
    access_token TEXT,
    refresh_token TEXT,
    expiry TEXT,
    updated_at TEXT
);


CREATE TABLE IF NOT EXISTS context_snapshots (
    hash TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

` + pushInstallationsTable + `

` + notificationsTable + `

CREATE TABLE IF NOT EXISTS knowledge_notes (
    path TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    tags TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    revision INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_revisions (
    path TEXT NOT NULL REFERENCES knowledge_notes(path),
    revision INTEGER NOT NULL,
    title TEXT NOT NULL,
    tags TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    body_sha256 TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    at TEXT NOT NULL,
    PRIMARY KEY (path, revision)
);

` + skillsTables + `

` + skillPackageTables + `

` + skillSecretsTable + `

` + skillTriggerTables + `

` + personaTable + `

` + ownerTable + `

` + threadFilesTable + `

` + runProcessesTable + `

CREATE TABLE IF NOT EXISTS watches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    selector TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL,
    status TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    next_check_at TEXT NOT NULL,
    last_checked_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    failures INTEGER NOT NULL DEFAULT 0,
    snapshot TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS watch_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    watch_id INTEGER NOT NULL,
    item_key TEXT NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    seen_at TEXT NOT NULL,
    UNIQUE(watch_id, item_key)
);

CREATE TABLE IF NOT EXISTS routine_runs (
    routine TEXT NOT NULL,
    slot TEXT NOT NULL,
    thread_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (routine, slot)
);
CREATE TABLE IF NOT EXISTS phone_notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    notification_key TEXT NOT NULL,
    package TEXT NOT NULL,
    app TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL DEFAULT '',
    posted_at TEXT NOT NULL,
    received_at TEXT NOT NULL,
    UNIQUE(device_id, notification_key, posted_at)
);
`

// storeBootstrapIndexes creates all indexes, full-text tables and their sync
// triggers after table creation and schema validation have succeeded, ensuring
// the columns they reference exist.
const storeBootstrapIndexes = `
CREATE INDEX IF NOT EXISTS idx_session_messages_run_id ON session_messages(run_id);
CREATE INDEX IF NOT EXISTS idx_session_messages_session_turn ON session_messages(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_runs_session_created ON runs(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_kind_created ON events(kind, created_at);
CREATE INDEX IF NOT EXISTS idx_phone_notifications_received ON phone_notifications(received_at);
CREATE INDEX IF NOT EXISTS idx_events_run_sequence ON events(run_id, sequence ASC);
CREATE INDEX IF NOT EXISTS idx_pending_actions_run_id_status ON pending_actions(run_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_actions_interrupt_id ON pending_actions(interrupt_id) WHERE interrupt_id <> '';
CREATE INDEX IF NOT EXISTS idx_devices_token_hash ON devices(token_hash);
CREATE INDEX IF NOT EXISTS idx_artifacts_run ON artifacts(run_id, created_at ASC, artifact_id ASC);
CREATE INDEX IF NOT EXISTS idx_artifacts_session ON artifacts(session_id, created_at ASC, artifact_id ASC);
` + notificationsDueIndex + `
CREATE INDEX IF NOT EXISTS idx_knowledge_notes_updated ON knowledge_notes(updated_at);
CREATE INDEX IF NOT EXISTS idx_watches_due ON watches(status, next_check_at);
CREATE INDEX IF NOT EXISTS idx_watch_items_status ON watch_items(status, seen_at);

CREATE VIRTUAL TABLE IF NOT EXISTS runs_fts USING fts5(run_id UNINDEXED, input_text, output_text, tokenize='trigram');
CREATE TRIGGER IF NOT EXISTS runs_fts_ai AFTER INSERT ON runs BEGIN
  INSERT INTO runs_fts(run_id, input_text, output_text) VALUES (new.run_id, new.input_text, new.output_text);
END;
CREATE TRIGGER IF NOT EXISTS runs_fts_au AFTER UPDATE OF input_text, output_text ON runs BEGIN
  DELETE FROM runs_fts WHERE run_id = new.run_id;
  INSERT INTO runs_fts(run_id, input_text, output_text) VALUES (new.run_id, new.input_text, new.output_text);
END;
` + runsFTSDeleteTrigger + `

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_notes_fts USING fts5(path UNINDEXED, title, tags, body, tokenize='trigram');
`

// runsFTSDeleteTrigger is shared with the migration that adds it.
const runsFTSDeleteTrigger = `CREATE TRIGGER IF NOT EXISTS runs_fts_ad AFTER DELETE ON runs BEGIN
  DELETE FROM runs_fts WHERE run_id = old.run_id;
END;`

// skillsTables is shared with the migration that adds them.
const skillsTables = `CREATE TABLE IF NOT EXISTS skills (
    id TEXT PRIMARY KEY,
    markdown TEXT NOT NULL,
    revision INTEGER NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0,
    seed_sha256 TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_revisions (
    id TEXT NOT NULL REFERENCES skills(id),
    revision INTEGER NOT NULL,
    markdown TEXT NOT NULL,
    markdown_sha256 TEXT NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0,
    author TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    at TEXT NOT NULL,
    PRIMARY KEY (id, revision)
);`

// skillPackageTables are shared with the migration that adds them: the files
// of each skill revision, and packages read for import waiting for approval.
const skillPackageTables = `CREATE TABLE IF NOT EXISTS skill_files (
    id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    path TEXT NOT NULL,
    content BLOB NOT NULL,
    size INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    PRIMARY KEY (id, revision, path),
    FOREIGN KEY (id, revision) REFERENCES skill_revisions(id, revision)
);

CREATE TABLE IF NOT EXISTS skill_package_stages (
    sha256 TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    markdown TEXT NOT NULL,
    staged_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_package_stage_files (
    sha256 TEXT NOT NULL REFERENCES skill_package_stages(sha256),
    path TEXT NOT NULL,
    content BLOB NOT NULL,
    PRIMARY KEY (sha256, path)
);`

// skillSecretsTable is shared with the migration that adds it: the values of
// the secrets a skill package's MCP server declares, by skill and name.
const skillSecretsTable = `CREATE TABLE IF NOT EXISTS skill_secrets (
    skill TEXT NOT NULL REFERENCES skills(id),
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (skill, name)
);`

// skillTriggerTables are shared with the migration that adds them: the
// schedule of each trigger of a skill in use, the events trigger commands
// printed, unique per skill, trigger and key, and the thread each skill's
// trigger runs happen in.
const skillTriggerTables = `CREATE TABLE IF NOT EXISTS skill_triggers (
    skill TEXT NOT NULL,
    name TEXT NOT NULL,
    schedule TEXT NOT NULL,
    status TEXT NOT NULL,
    next_at TEXT NOT NULL,
    failures INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    last_run_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (skill, name)
);

CREATE TABLE IF NOT EXISTS skill_trigger_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    skill TEXT NOT NULL,
    trigger_name TEXT NOT NULL,
    event_key TEXT NOT NULL,
    text TEXT NOT NULL,
    status TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    seen_at TEXT NOT NULL,
    UNIQUE (skill, trigger_name, event_key)
);

CREATE INDEX IF NOT EXISTS skill_trigger_events_status ON skill_trigger_events(status, id);

CREATE TABLE IF NOT EXISTS skill_threads (
    skill TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL
);`

// personaTable is shared with the migration that adds it. The newest
// revision is the current persona.
const personaTable = `CREATE TABLE IF NOT EXISTS persona_revisions (
    revision INTEGER PRIMARY KEY,
    content TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    author TEXT NOT NULL,
    device_id TEXT NOT NULL DEFAULT '',
    at TEXT NOT NULL
);`

// ownerTable is shared with the migration that adds it. Its one row holds
// what Acorn calls the owner; no row means the owner is unnamed.
const ownerTable = `CREATE TABLE IF NOT EXISTS owner (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

// pushInstallationsTable is shared with the migration that adds it. Each row
// holds the Firebase installation ID FCM delivers a device's pushes to.
const pushInstallationsTable = `CREATE TABLE IF NOT EXISTS push_installations (
    device_id TEXT PRIMARY KEY,
    fid TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

// notificationsTable is shared with the migrations that rebuild the table, so
// an upgraded database and a new one store the same definition.
const notificationsTable = `CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    thread_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    send_after TEXT NOT NULL,
    error_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    sent_at TEXT NOT NULL DEFAULT '',
    action_id TEXT NOT NULL DEFAULT '',
    browser_url TEXT NOT NULL DEFAULT ''
);`

const notificationsDueIndex = `CREATE INDEX IF NOT EXISTS idx_notifications_due ON notifications(status, send_after);`
