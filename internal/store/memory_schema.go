package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) createMemorySchema() error {
	ctx := context.Background()
	for _, ddl := range []string{memorySchema, memoryQueryIndexes, memorySourceTriggers} {
		if _, err := s.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create memory schema: %w", err)
		}
	}
	return s.validateMemorySchema()
}

const memorySchema = `
CREATE TABLE IF NOT EXISTS memory_sources (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, object_id TEXT NOT NULL, version TEXT NOT NULL,
 speaker TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '', run_id TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL DEFAULT '', recorded_at TEXT NOT NULL, occurred_at TEXT NOT NULL DEFAULT '',
 UNIQUE(kind, object_id, version)
);
CREATE TABLE IF NOT EXISTS memory_records (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, content TEXT NOT NULL, scope TEXT NOT NULL,
 state TEXT NOT NULL, basis TEXT NOT NULL, revision INTEGER NOT NULL, recorded_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 valid_from TEXT NOT NULL DEFAULT '', valid_to TEXT NOT NULL DEFAULT '', needs_review INTEGER NOT NULL DEFAULT 0,
 pinned INTEGER NOT NULL DEFAULT 0, excluded INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS memory_revisions (
 record_id TEXT NOT NULL REFERENCES memory_records(id), revision INTEGER NOT NULL,
 kind TEXT NOT NULL, content TEXT NOT NULL, scope TEXT NOT NULL, state TEXT NOT NULL, basis TEXT NOT NULL,
 valid_from TEXT NOT NULL DEFAULT '', valid_to TEXT NOT NULL DEFAULT '', needs_review INTEGER NOT NULL DEFAULT 0,
 pinned INTEGER NOT NULL DEFAULT 0, at TEXT NOT NULL, reason TEXT NOT NULL, PRIMARY KEY(record_id,revision)
);
-- A revision's evidence, entities, parents and resolved source IDs; rowid keeps
-- their order.
CREATE TABLE IF NOT EXISTS memory_revision_evidence (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, source_id TEXT NOT NULL REFERENCES memory_sources(id),
 quote TEXT NOT NULL, relation TEXT NOT NULL CHECK(relation IN ('supports','contradicts')),
 UNIQUE(record_id, revision, source_id, quote, relation),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE IF NOT EXISTS memory_revision_entities (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, entity TEXT NOT NULL,
 UNIQUE(record_id, revision, entity),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE IF NOT EXISTS memory_revision_parents (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, parent_id TEXT NOT NULL REFERENCES memory_records(id),
 UNIQUE(record_id, revision, parent_id),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE IF NOT EXISTS memory_revision_sources (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, source_id TEXT NOT NULL,
 UNIQUE(record_id, revision, source_id),
 FOREIGN KEY(record_id, revision) REFERENCES memory_revisions(record_id, revision)
);
CREATE TABLE IF NOT EXISTS memory_links (
 from_id TEXT NOT NULL REFERENCES memory_records(id), to_id TEXT NOT NULL REFERENCES memory_records(id),
 relation TEXT NOT NULL, CHECK(from_id <> to_id), PRIMARY KEY(from_id,to_id,relation)
);
CREATE TABLE IF NOT EXISTS memory_entities (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS memory_mentions (
 record_id TEXT NOT NULL REFERENCES memory_records(id), entity_id INTEGER NOT NULL REFERENCES memory_entities(id),
 PRIMARY KEY(record_id,entity_id)
);
CREATE TABLE IF NOT EXISTS memory_concerns (
 id TEXT PRIMARY KEY, title TEXT NOT NULL, state TEXT NOT NULL, revision INTEGER NOT NULL,
 source_id TEXT NOT NULL REFERENCES memory_sources(id), data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS memory_concern_links (
 concern_id TEXT NOT NULL REFERENCES memory_concerns(id), record_id TEXT NOT NULL REFERENCES memory_records(id),
 PRIMARY KEY(concern_id,record_id)
);
CREATE TABLE IF NOT EXISTS memory_concern_revisions (
 concern_id TEXT NOT NULL REFERENCES memory_concerns(id), revision INTEGER NOT NULL, data TEXT NOT NULL,
 PRIMARY KEY(concern_id,revision)
);
CREATE TABLE IF NOT EXISTS commitments (
 id INTEGER PRIMARY KEY AUTOINCREMENT, content TEXT NOT NULL, state TEXT NOT NULL,
 session_id TEXT NOT NULL DEFAULT '', source_run_id TEXT NOT NULL DEFAULT '', concern_id TEXT NOT NULL DEFAULT '',
 wake_at TEXT NOT NULL, recurrence TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS commitment_occurrences (
 id INTEGER PRIMARY KEY AUTOINCREMENT, commitment_id INTEGER NOT NULL REFERENCES commitments(id),
 due_at TEXT NOT NULL, state TEXT NOT NULL, run_id TEXT NOT NULL DEFAULT '', evidence_source_id TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL, UNIQUE(commitment_id,due_at)
);
` + memoryVectorSchema + memoryAliasSchema + `
CREATE TABLE IF NOT EXISTS memory_index_state (
 id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL, model TEXT NOT NULL,
 dimensions INTEGER NOT NULL, state TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS memory_jobs (
 id INTEGER PRIMARY KEY AUTOINCREMENT, operation TEXT NOT NULL, object_id TEXT NOT NULL, version TEXT NOT NULL,
 processor_version TEXT NOT NULL DEFAULT '1', state TEXT NOT NULL DEFAULT 'pending',
 token INTEGER NOT NULL DEFAULT 0, epoch INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0, cursor INTEGER NOT NULL DEFAULT 0,
 lease_until TEXT NOT NULL DEFAULT '', available_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, completed_at TEXT NOT NULL DEFAULT '',
 UNIQUE(operation,object_id,version,processor_version)
);
CREATE TABLE IF NOT EXISTS memory_state (id INTEGER PRIMARY KEY CHECK(id=1), epoch INTEGER NOT NULL, request_run_id TEXT NOT NULL DEFAULT '');
INSERT OR IGNORE INTO memory_state(id,epoch) VALUES(1,0);
CREATE TABLE IF NOT EXISTS memory_exclusions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, epoch INTEGER NOT NULL, source_id TEXT NOT NULL REFERENCES memory_sources(id),
 quote TEXT NOT NULL DEFAULT '', request_source_id TEXT NOT NULL REFERENCES memory_sources(id),
 reason TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(source_id,quote)
);
CREATE TABLE IF NOT EXISTS memory_source_links (
 source_id TEXT NOT NULL REFERENCES memory_sources(id), parent_id TEXT NOT NULL REFERENCES memory_sources(id),
 CHECK(source_id <> parent_id), PRIMARY KEY(source_id,parent_id)
);
CREATE TABLE IF NOT EXISTS thread_summaries (
 session_id TEXT PRIMARY KEY, through_message_id INTEGER NOT NULL, epoch INTEGER NOT NULL,
 content TEXT NOT NULL, sources_json TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS context_snapshot_refs (
 hash TEXT NOT NULL, run_id TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '', record_id TEXT NOT NULL DEFAULT '',
 epoch INTEGER NOT NULL, PRIMARY KEY(hash,run_id,source_id,record_id)
);
CREATE TABLE IF NOT EXISTS memory_usage (
 id TEXT PRIMARY KEY, operation TEXT NOT NULL, job_id INTEGER NOT NULL DEFAULT 0,
 run_id TEXT NOT NULL DEFAULT '', budget TEXT NOT NULL, model TEXT NOT NULL,
 input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL, reported INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_jobs_ready ON memory_jobs(state,available_at,lease_until,id);
CREATE INDEX IF NOT EXISTS idx_memory_sources_run ON memory_sources(run_id);
CREATE INDEX IF NOT EXISTS idx_memory_sources_session ON memory_sources(session_id,recorded_at);
CREATE INDEX IF NOT EXISTS idx_memory_revision_evidence_source ON memory_revision_evidence(source_id,record_id,revision);
CREATE INDEX IF NOT EXISTS idx_memory_revision_sources_source ON memory_revision_sources(source_id,record_id,revision);
CREATE INDEX IF NOT EXISTS idx_memory_links_parent ON memory_links(to_id,relation,from_id);
CREATE INDEX IF NOT EXISTS idx_memory_source_links_parent ON memory_source_links(parent_id,source_id);
CREATE INDEX IF NOT EXISTS idx_commitments_due ON commitments(state,wake_at);
CREATE INDEX IF NOT EXISTS idx_memory_records_current ON memory_records(excluded,state,recorded_at);
CREATE INDEX IF NOT EXISTS idx_memory_usage_budget ON memory_usage(budget,created_at);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_records_fts USING fts5(content,scope,content='memory_records',content_rowid='rowid',tokenize='trigram');
CREATE TRIGGER IF NOT EXISTS memory_records_fts_ai AFTER INSERT ON memory_records BEGIN
 INSERT INTO memory_records_fts(rowid,content,scope) VALUES(new.rowid,new.content,new.scope);
END;
CREATE TRIGGER IF NOT EXISTS memory_records_fts_au AFTER UPDATE OF content,scope ON memory_records BEGIN
 INSERT INTO memory_records_fts(memory_records_fts,rowid,content,scope) VALUES('delete',old.rowid,old.content,old.scope);
 INSERT INTO memory_records_fts(rowid,content,scope) VALUES(new.rowid,new.content,new.scope);
END;
`

const memoryQueryIndexes = `
CREATE INDEX IF NOT EXISTS idx_memory_records_vector_current ON memory_records(id,revision,excluded);
CREATE INDEX IF NOT EXISTS idx_memory_mentions_entity ON memory_mentions(entity_id,record_id);
CREATE INDEX IF NOT EXISTS idx_memory_jobs_completed ON memory_jobs(completed_at);
`

const memoryVectorSchema = `
CREATE TABLE IF NOT EXISTS memory_embeddings (
 record_id TEXT NOT NULL REFERENCES memory_records(id), revision INTEGER NOT NULL, generation INTEGER NOT NULL,
 dimensions INTEGER NOT NULL, vector BLOB NOT NULL, PRIMARY KEY(record_id,revision,generation)
);
CREATE TABLE IF NOT EXISTS memory_vector_sketches (
 generation INTEGER NOT NULL, record_id TEXT NOT NULL, revision INTEGER NOT NULL,
 dimensions INTEGER NOT NULL, scale REAL NOT NULL, codes BLOB NOT NULL,
 PRIMARY KEY(generation,record_id,revision),
 FOREIGN KEY(record_id,revision,generation) REFERENCES memory_embeddings(record_id,revision,generation) ON DELETE CASCADE
);
`

const memorySourceTriggers = `
CREATE TRIGGER IF NOT EXISTS memory_source_link_ai AFTER INSERT ON memory_source_links BEGIN
 INSERT OR IGNORE INTO memory_exclusions(epoch,source_id,quote,request_source_id,reason,created_at)
 SELECT x.epoch,new.source_id,'',x.request_source_id,'derived from an excluded source',x.created_at
 FROM memory_exclusions x WHERE x.source_id=new.parent_id ORDER BY x.epoch DESC LIMIT 1;
 UPDATE memory_jobs SET state='done',error='owner excluded source',lease_until=''
 WHERE object_id=new.source_id AND EXISTS(SELECT 1 FROM memory_exclusions WHERE source_id=new.source_id AND quote='');
END;
CREATE TRIGGER IF NOT EXISTS memory_watch_ai AFTER INSERT ON watch_items BEGIN
 INSERT INTO memory_sources(id,kind,object_id,version,speaker,session_id,recorded_at,occurred_at)
 VALUES('watch:'||new.id,'watch',CAST(new.id AS TEXT),'1','external',COALESCE((SELECT session_id FROM watches WHERE id=new.watch_id),''),new.seen_at,new.published_at);
 INSERT OR IGNORE INTO memory_jobs(operation,object_id,version,available_at,created_at)
 VALUES('extract','watch:'||new.id,'1',new.seen_at,new.seen_at);
END;

CREATE TRIGGER IF NOT EXISTS memory_message_ai AFTER INSERT ON session_messages BEGIN
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
CREATE TRIGGER IF NOT EXISTS memory_message_run_au AFTER UPDATE OF run_id ON session_messages BEGIN
 UPDATE memory_sources SET run_id=new.run_id WHERE id='message:'||new.id;
END;
CREATE TRIGGER IF NOT EXISTS memory_event_ai AFTER INSERT ON events
 WHEN new.kind IN ('tool.call.succeeded','tool.call.failed') BEGIN
 INSERT INTO memory_sources(id,kind,object_id,version,speaker,session_id,run_id,recorded_at)
 VALUES('event:'||new.sequence,'event',CAST(new.sequence AS TEXT),'1','tool',
 COALESCE((SELECT session_id FROM runs WHERE run_id=new.run_id),''),new.run_id,new.created_at);
 INSERT OR IGNORE INTO memory_jobs(operation,object_id,version,available_at,created_at)
 VALUES('extract','event:'||new.sequence,'1',new.created_at,new.created_at);
 INSERT OR IGNORE INTO memory_source_links(source_id,parent_id)
 SELECT 'event:'||new.sequence,ref.source_id FROM context_snapshot_refs ref
 WHERE ref.run_id=new.run_id AND ref.source_id<>'' AND ref.source_id<>'event:'||new.sequence;

END;
`

// memorySQL keeps transaction-scoped operations on the same connection.
type memorySQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) memoryTransaction(ctx context.Context, fn func(*sql.Tx) error) (resultErr error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin memory transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback memory transaction: %w", err))
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory transaction: %w", err)
	}
	return nil
}
