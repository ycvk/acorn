CREATE TABLE agent_checkpoints (
    checkpoint_id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE artifacts (
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
CREATE TABLE commitment_occurrences (
 id INTEGER PRIMARY KEY AUTOINCREMENT, commitment_id INTEGER NOT NULL REFERENCES commitments(id),
 due_at TEXT NOT NULL, state TEXT NOT NULL, run_id TEXT NOT NULL DEFAULT '', evidence_source_id TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL, UNIQUE(commitment_id,due_at)
);
CREATE TABLE commitments (
 id INTEGER PRIMARY KEY AUTOINCREMENT, content TEXT NOT NULL, state TEXT NOT NULL,
 session_id TEXT NOT NULL DEFAULT '', source_run_id TEXT NOT NULL DEFAULT '', concern_id TEXT NOT NULL DEFAULT '',
 wake_at TEXT NOT NULL, recurrence TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE context_snapshot_refs (
 hash TEXT NOT NULL, run_id TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '', record_id TEXT NOT NULL DEFAULT '',
 epoch INTEGER NOT NULL, PRIMARY KEY(hash,run_id,source_id,record_id)
);
CREATE TABLE context_snapshots (
    hash TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE devices (
    device_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    platform TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT '',
    revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE knowledge_notes (
    path TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    tags TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    revision INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE knowledge_notes_fts USING fts5(path UNINDEXED, title, tags, body, tokenize='trigram');
CREATE TABLE knowledge_revisions (
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
CREATE TABLE mcp_oauth_tokens (
    provider_name TEXT PRIMARY KEY,
    access_token TEXT,
    refresh_token TEXT,
    expiry TEXT,
    updated_at TEXT
);
CREATE TABLE memory_concern_records (
 concern_id TEXT NOT NULL, revision INTEGER NOT NULL, record_id TEXT NOT NULL REFERENCES memory_records(id),
 UNIQUE(concern_id, revision, record_id),
 FOREIGN KEY(concern_id, revision) REFERENCES memory_concern_revisions(concern_id, revision)
);
CREATE TABLE memory_concern_revisions (
 concern_id TEXT NOT NULL REFERENCES memory_concerns(id), revision INTEGER NOT NULL,
 title TEXT NOT NULL, state TEXT NOT NULL, reason TEXT NOT NULL, source_id TEXT NOT NULL REFERENCES memory_sources(id),
 review_at TEXT NOT NULL DEFAULT '', at TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT '', PRIMARY KEY(concern_id,revision)
);
CREATE TABLE memory_concerns (
 id TEXT PRIMARY KEY, title TEXT NOT NULL, state TEXT NOT NULL, reason TEXT NOT NULL,
 source_id TEXT NOT NULL REFERENCES memory_sources(id), revision INTEGER NOT NULL,
 review_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 thread_id TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT ''
);
CREATE TABLE memory_embeddings (
 record_id TEXT NOT NULL REFERENCES memory_records(id), revision INTEGER NOT NULL, generation INTEGER NOT NULL,
 dimensions INTEGER NOT NULL, vector BLOB NOT NULL, PRIMARY KEY(record_id,revision,generation)
);
CREATE TABLE memory_entities (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
CREATE TABLE memory_entity_aliases (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, canonical TEXT NOT NULL,
 alias TEXT NOT NULL, scope TEXT NOT NULL, source_id TEXT NOT NULL, quote TEXT NOT NULL,
 PRIMARY KEY(record_id,revision,canonical,alias,scope),
 FOREIGN KEY(record_id,revision) REFERENCES memory_revisions(record_id,revision),
 FOREIGN KEY(source_id) REFERENCES memory_sources(id)
);
CREATE TABLE memory_exclusions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, epoch INTEGER NOT NULL, source_id TEXT NOT NULL REFERENCES memory_sources(id),
 quote TEXT NOT NULL DEFAULT '', request_source_id TEXT NOT NULL REFERENCES memory_sources(id),
 reason TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(source_id,quote)
);
CREATE TABLE memory_index_state (
 id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL, model TEXT NOT NULL,
 dimensions INTEGER NOT NULL, state TEXT NOT NULL
);
CREATE TABLE memory_jobs (
 id INTEGER PRIMARY KEY AUTOINCREMENT, operation TEXT NOT NULL, object_id TEXT NOT NULL, version TEXT NOT NULL,
 processor_version TEXT NOT NULL DEFAULT '1', state TEXT NOT NULL DEFAULT 'pending',
 token INTEGER NOT NULL DEFAULT 0, epoch INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0, cursor INTEGER NOT NULL DEFAULT 0,
 lease_until TEXT NOT NULL DEFAULT '', available_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, completed_at TEXT NOT NULL DEFAULT '',
 UNIQUE(operation,object_id,version,processor_version)
);
CREATE TABLE memory_links (
 from_id TEXT NOT NULL REFERENCES memory_records(id), to_id TEXT NOT NULL REFERENCES memory_records(id),
 relation TEXT NOT NULL, CHECK(from_id <> to_id), PRIMARY KEY(from_id,to_id,relation)
);
CREATE TABLE memory_mentions (
 record_id TEXT NOT NULL REFERENCES memory_records(id), entity_id INTEGER NOT NULL REFERENCES memory_entities(id),
 PRIMARY KEY(record_id,entity_id)
);
CREATE TABLE memory_records (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, content TEXT NOT NULL, scope TEXT NOT NULL,
 state TEXT NOT NULL, basis TEXT NOT NULL, revision INTEGER NOT NULL, recorded_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 valid_from TEXT NOT NULL DEFAULT '', valid_to TEXT NOT NULL DEFAULT '', needs_review INTEGER NOT NULL DEFAULT 0,
 pinned INTEGER NOT NULL DEFAULT 0, excluded INTEGER NOT NULL DEFAULT 0
);
CREATE VIRTUAL TABLE memory_records_fts USING fts5(content,scope,content='memory_records',content_rowid='rowid',tokenize='trigram');
CREATE TABLE memory_revision_entities (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, entity TEXT NOT NULL,
 UNIQUE(record_id, revision, entity),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE memory_revision_evidence (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, source_id TEXT NOT NULL REFERENCES memory_sources(id),
 quote TEXT NOT NULL, relation TEXT NOT NULL CHECK(relation IN ('supports','contradicts')),
 UNIQUE(record_id, revision, source_id, quote, relation),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE memory_revision_parents (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, parent_id TEXT NOT NULL REFERENCES memory_records(id),
 UNIQUE(record_id, revision, parent_id),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE memory_revision_sources (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, source_id TEXT NOT NULL,
 UNIQUE(record_id, revision, source_id),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE memory_revisions (
 record_id TEXT NOT NULL REFERENCES memory_records(id), revision INTEGER NOT NULL,
 kind TEXT NOT NULL, content TEXT NOT NULL, scope TEXT NOT NULL, state TEXT NOT NULL, basis TEXT NOT NULL,
 valid_from TEXT NOT NULL DEFAULT '', valid_to TEXT NOT NULL DEFAULT '', needs_review INTEGER NOT NULL DEFAULT 0,
 pinned INTEGER NOT NULL DEFAULT 0, at TEXT NOT NULL, reason TEXT NOT NULL, PRIMARY KEY(record_id,revision)
);
CREATE TABLE memory_source_links (
 source_id TEXT NOT NULL REFERENCES memory_sources(id), parent_id TEXT NOT NULL REFERENCES memory_sources(id),
 CHECK(source_id <> parent_id), PRIMARY KEY(source_id,parent_id)
);
CREATE TABLE memory_sources (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, object_id TEXT NOT NULL, version TEXT NOT NULL,
 speaker TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '', run_id TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL DEFAULT '', recorded_at TEXT NOT NULL, occurred_at TEXT NOT NULL DEFAULT '',
 UNIQUE(kind, object_id, version)
);
CREATE TABLE memory_state (id INTEGER PRIMARY KEY CHECK(id=1), epoch INTEGER NOT NULL, request_run_id TEXT NOT NULL DEFAULT '');
CREATE TABLE memory_usage (
 id TEXT PRIMARY KEY, operation TEXT NOT NULL, job_id INTEGER NOT NULL DEFAULT 0,
 run_id TEXT NOT NULL DEFAULT '', budget TEXT NOT NULL, model TEXT NOT NULL,
 input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL, reported INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE memory_vector_sketches (
 generation INTEGER NOT NULL, record_id TEXT NOT NULL, revision INTEGER NOT NULL,
 dimensions INTEGER NOT NULL, scale REAL NOT NULL, codes BLOB NOT NULL,
 PRIMARY KEY(generation,record_id,revision),
 FOREIGN KEY(record_id,revision,generation) REFERENCES memory_embeddings(record_id,revision,generation) ON DELETE CASCADE
);
CREATE TABLE notifications (
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
);
CREATE TABLE owner (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE pairing_codes (
    code_hash TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL,
    used_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE pending_actions (
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
CREATE TABLE persona_revisions (
    revision INTEGER PRIMARY KEY,
    content TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    author TEXT NOT NULL,
    device_id TEXT NOT NULL DEFAULT '',
    at TEXT NOT NULL
);
CREATE TABLE phone_notifications (
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
CREATE TABLE push_installations (
    device_id TEXT PRIMARY KEY,
    fid TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE routine_runs (
    routine TEXT NOT NULL,
    slot TEXT NOT NULL,
    thread_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (routine, slot)
);
CREATE TABLE run_processes (
    run_id TEXT PRIMARY KEY,
    process TEXT NOT NULL
);
CREATE TABLE runs (
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
CREATE VIRTUAL TABLE runs_fts USING fts5(run_id UNINDEXED, input_text, output_text, tokenize='trigram');
CREATE TABLE session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    turn_index INTEGER NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE sessions (
    session_id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE skill_files (
    id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    path TEXT NOT NULL,
    content BLOB NOT NULL,
    size INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    PRIMARY KEY (id, revision, path),
    FOREIGN KEY (id, revision) REFERENCES skill_revisions(id, revision)
);
CREATE TABLE skill_package_stage_files (
    sha256 TEXT NOT NULL REFERENCES skill_package_stages(sha256),
    path TEXT NOT NULL,
    content BLOB NOT NULL,
    PRIMARY KEY (sha256, path)
);
CREATE TABLE skill_package_stages (
    sha256 TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    markdown TEXT NOT NULL,
    staged_at TEXT NOT NULL
);
CREATE TABLE skill_revisions (
    id TEXT NOT NULL REFERENCES skills(id),
    revision INTEGER NOT NULL,
    markdown TEXT NOT NULL,
    markdown_sha256 TEXT NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0,
    author TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    at TEXT NOT NULL,
    PRIMARY KEY (id, revision)
);
CREATE TABLE skill_secrets (
    skill TEXT NOT NULL REFERENCES skills(id),
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (skill, name)
);
CREATE TABLE skill_threads (
    skill TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL
);
CREATE TABLE skill_trigger_events (
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
CREATE TABLE skill_triggers (
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
CREATE TABLE skills (
    id TEXT PRIMARY KEY,
    markdown TEXT NOT NULL,
    revision INTEGER NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0,
    seed_sha256 TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE thread_files (
    file_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    message_id INTEGER NOT NULL DEFAULT 0,
    run_id TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL CHECK (origin IN ('owner','agent')),
    name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE thread_summaries (
 session_id TEXT PRIMARY KEY, through_message_id INTEGER NOT NULL, epoch INTEGER NOT NULL,
 content TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE thread_summary_sources (
 session_id TEXT NOT NULL REFERENCES thread_summaries(session_id), source_id TEXT NOT NULL REFERENCES memory_sources(id),
 PRIMARY KEY(session_id, source_id)
);
CREATE TABLE thread_view_actions (
    view_id TEXT NOT NULL,
    action_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    component_id TEXT NOT NULL,
    scope_json TEXT NOT NULL,
    name TEXT NOT NULL,
    selected_json TEXT NOT NULL,
    context_json TEXT NOT NULL,
    message_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (view_id, action_key)
);
CREATE TABLE thread_views (
    view_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    title TEXT NOT NULL,
    catalog TEXT NOT NULL,
    components_json TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE watch_items (
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
CREATE TABLE watches (
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
CREATE INDEX idx_artifacts_run ON artifacts(run_id, created_at ASC, artifact_id ASC);
CREATE INDEX idx_artifacts_session ON artifacts(session_id, created_at ASC, artifact_id ASC);
CREATE INDEX idx_commitments_due ON commitments(state,wake_at);
CREATE INDEX idx_devices_token_hash ON devices(token_hash);
CREATE INDEX idx_events_kind_created ON events(kind, created_at);
CREATE INDEX idx_events_run_sequence ON events(run_id, sequence ASC);
CREATE INDEX idx_knowledge_notes_updated ON knowledge_notes(updated_at);
CREATE INDEX idx_memory_entity_aliases_name ON memory_entity_aliases(alias,scope,canonical);
CREATE INDEX idx_memory_jobs_completed ON memory_jobs(completed_at);
CREATE INDEX idx_memory_jobs_ready ON memory_jobs(state,available_at,lease_until,id);
CREATE INDEX idx_memory_links_parent ON memory_links(to_id,relation,from_id);
CREATE INDEX idx_memory_mentions_entity ON memory_mentions(entity_id,record_id);
CREATE INDEX idx_memory_records_current ON memory_records(excluded,state,recorded_at);
CREATE INDEX idx_memory_records_vector_current ON memory_records(id,revision,excluded);
CREATE INDEX idx_memory_revision_evidence_source ON memory_revision_evidence(source_id,record_id,revision);
CREATE INDEX idx_memory_revision_sources_source ON memory_revision_sources(source_id,record_id,revision);
CREATE INDEX idx_memory_source_links_parent ON memory_source_links(parent_id,source_id);
CREATE INDEX idx_memory_sources_run ON memory_sources(run_id);
CREATE INDEX idx_memory_sources_session ON memory_sources(session_id,recorded_at);
CREATE INDEX idx_memory_usage_budget ON memory_usage(budget,created_at);
CREATE INDEX idx_notifications_due ON notifications(status, send_after);
CREATE UNIQUE INDEX idx_pending_actions_interrupt_id ON pending_actions(interrupt_id) WHERE interrupt_id <> '';
CREATE INDEX idx_pending_actions_run_id_status ON pending_actions(run_id, status, created_at DESC);
CREATE INDEX idx_phone_notifications_received ON phone_notifications(received_at);
CREATE INDEX idx_runs_session_created ON runs(session_id, created_at DESC);
CREATE INDEX idx_session_messages_run_id ON session_messages(run_id);
CREATE INDEX idx_session_messages_session_turn ON session_messages(session_id, turn_index);
CREATE INDEX idx_thread_files_session ON thread_files(session_id, created_at);
CREATE INDEX idx_thread_summary_sources_source ON thread_summary_sources(source_id,session_id);
CREATE INDEX idx_thread_view_actions_session ON thread_view_actions(session_id, created_at);
CREATE INDEX idx_thread_views_session ON thread_views(session_id, created_at);
CREATE INDEX idx_watch_items_status ON watch_items(status, seen_at);
CREATE INDEX idx_watches_due ON watches(status, next_check_at);
CREATE INDEX skill_trigger_events_status ON skill_trigger_events(status, id);
CREATE TRIGGER memory_event_ai AFTER INSERT ON events
 WHEN new.kind IN ('tool.call.succeeded','tool.call.failed') BEGIN
 INSERT INTO memory_sources(id,kind,object_id,version,speaker,session_id,run_id,recorded_at)
 VALUES('event:'||new.sequence,'event',CAST(new.sequence AS TEXT),'1','tool',
 COALESCE((SELECT session_id FROM runs WHERE run_id=new.run_id),''),new.run_id,new.created_at);
 INSERT OR IGNORE INTO memory_source_links(source_id,parent_id)
 SELECT 'event:'||new.sequence,ref.source_id FROM context_snapshot_refs ref
 WHERE ref.run_id=new.run_id AND ref.source_id<>'' AND ref.source_id<>'event:'||new.sequence;
END;
CREATE TRIGGER memory_message_ai AFTER INSERT ON session_messages BEGIN
 INSERT INTO memory_sources(id,kind,object_id,version,speaker,session_id,run_id,recorded_at)
 VALUES('message:'||new.id,'message',CAST(new.id AS TEXT),'1',
 CASE WHEN new.role IN ('user','capture') THEN 'owner' WHEN new.role='assistant' THEN 'assistant' ELSE 'system' END,
 new.session_id,new.run_id,new.created_at);
 INSERT OR IGNORE INTO memory_jobs(operation,object_id,version,available_at,created_at)
 SELECT 'extract','message:'||new.id,'1',new.created_at,new.created_at WHERE new.role IN ('user','capture');
 INSERT OR IGNORE INTO memory_source_links(source_id,parent_id)
 SELECT 'message:'||new.id,ref.source_id FROM context_snapshot_refs ref
 WHERE new.role='assistant' AND ref.run_id=new.run_id AND ref.source_id<>'';
END;
CREATE TRIGGER memory_message_run_au AFTER UPDATE OF run_id ON session_messages BEGIN
 UPDATE memory_sources SET run_id=new.run_id WHERE id='message:'||new.id;
END;
CREATE TRIGGER memory_source_link_ai AFTER INSERT ON memory_source_links BEGIN
 INSERT OR IGNORE INTO memory_exclusions(epoch,source_id,quote,request_source_id,reason,created_at)
 SELECT x.epoch,new.source_id,'',x.request_source_id,'derived from an excluded source',x.created_at
 FROM memory_exclusions x WHERE x.source_id=new.parent_id AND (x.quote='' OR EXISTS(SELECT 1 FROM memory_sources s
 JOIN context_snapshot_refs r ON r.run_id=s.run_id AND r.source_id=new.parent_id WHERE s.id=new.source_id AND s.run_id<>'' AND r.epoch<x.epoch))
 ORDER BY x.epoch DESC LIMIT 1;
 UPDATE memory_jobs SET state='done',error='owner excluded source',lease_until=''
 WHERE object_id=new.source_id AND EXISTS(SELECT 1 FROM memory_exclusions WHERE source_id=new.source_id AND quote='');
END;
CREATE TRIGGER memory_trigger_event_ai AFTER INSERT ON skill_trigger_events BEGIN
 INSERT INTO memory_sources(id,kind,object_id,version,speaker,recorded_at)
 VALUES('trigger:'||new.id,'trigger',CAST(new.id AS TEXT),'1','external',new.seen_at);
END;
CREATE TRIGGER memory_watch_ai AFTER INSERT ON watch_items BEGIN
 INSERT INTO memory_sources(id,kind,object_id,version,speaker,session_id,recorded_at,occurred_at)
 VALUES('watch:'||new.id,'watch',CAST(new.id AS TEXT),'1','external',COALESCE((SELECT session_id FROM watches WHERE id=new.watch_id),''),new.seen_at,new.published_at);
END;
