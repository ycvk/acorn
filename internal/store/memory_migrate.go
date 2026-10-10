package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) migratePersonalMemory() error {
	ctx := context.Background()
	const version = "v6_personal_memory"
	if !migrationApplied(s.db, version) {
		err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, memorySchema); err != nil {
				return err
			}
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='memory_items'`).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				var active int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE status IN ('running','interrupted')`).Scan(&active); err != nil {
					return err
				}
				if active > 0 {
					return fmt.Errorf("%w: memory migration requires finishing or cancelling active and interrupted runs", core.ErrUnsupportedStorageSchema)
				}
				if err := migrateMemoryItems(ctx, tx); err != nil {
					return err
				}
				for _, statement := range []string{`DROP TRIGGER IF EXISTS memory_items_fts_ai`, `DROP TRIGGER IF EXISTS memory_items_fts_au`, `DROP TABLE IF EXISTS memory_items_fts`, `DROP INDEX IF EXISTS idx_memory_items_due`, `DROP TABLE memory_items`} {
					if _, err := tx.ExecContext(ctx, statement); err != nil {
						return err
					}
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_sources(id,kind,object_id,version,speaker,session_id,run_id,recorded_at)
			SELECT 'message:'||id,'message',CAST(id AS TEXT),'1',CASE WHEN role IN ('user','capture') THEN 'owner' WHEN role='assistant' THEN 'assistant' ELSE 'system' END,session_id,run_id,created_at FROM session_messages`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_sources(id,kind,object_id,version,speaker,session_id,run_id,recorded_at)
			SELECT 'event:'||e.sequence,'event',CAST(e.sequence AS TEXT),'1','tool',COALESCE(r.session_id,''),e.run_id,e.created_at FROM events e LEFT JOIN runs r ON r.run_id=e.run_id WHERE e.kind IN ('tool.call.succeeded','tool.call.failed')`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_sources(id,kind,object_id,version,speaker,session_id,recorded_at,occurred_at)
 SELECT 'watch:'||i.id,'watch',CAST(i.id AS TEXT),'1','external',COALESCE(w.session_id,''),i.seen_at,i.published_at FROM watch_items i LEFT JOIN watches w ON w.id=i.watch_id`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_jobs(operation,object_id,version,available_at,created_at) SELECT 'extract',id,version,recorded_at,recorded_at FROM memory_sources WHERE speaker IN ('owner','tool','external')`); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`, version, formatTimestamp(time.Now().UTC()))
			return err
		})
		if err != nil {
			return fmt.Errorf("migrate personal memory: %w", err)
		}
	}
	if err := s.migrateMemoryAliases(); err != nil {
		return err
	}
	if err := s.validateMemorySchema(); err != nil {
		return err
	}
	if err := s.migrateMemoryRevisionVectors(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, memoryQueryIndexes); err != nil {
		return fmt.Errorf("memory query indexes: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, memorySourceTriggers); err != nil {
		return fmt.Errorf("memory source triggers: %w", err)
	}
	return nil
}

type importedMemory struct {
	id                                                                     int64
	kind, content, state, session, run, wake, recurrence, created, updated string
}

func migrateMemoryItems(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,content,status,session_id,source_run_id,wake_at,recurrence,created_at,updated_at FROM memory_items ORDER BY id`)
	if err != nil {
		return err
	}
	var items []importedMemory
	if err := scanRows(rows, func(scan func(...any) error) error {
		var i importedMemory
		if err := scan(&i.id, &i.kind, &i.content, &i.state, &i.session, &i.run, &i.wake, &i.recurrence, &i.created, &i.updated); err != nil {
			return err
		}
		items = append(items, i)
		return nil
	}); err != nil {
		return err
	}
	for _, i := range items {
		if i.kind == "commitment" {
			if err := migrateCommitment(ctx, tx, i); err != nil {
				return err
			}
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, i.created)
		if err != nil {
			return err
		}
		updated, err := time.Parse(time.RFC3339Nano, i.updated)
		if err != nil {
			return err
		}
		source := core.MemorySource{ID: fmt.Sprintf("import:%d", i.id), Kind: "import", ObjectID: fmt.Sprint(i.id), Version: "1", Speaker: "import", Body: i.content, SessionID: i.session, RunID: i.run, RecordedAt: created}
		if err := registerMemorySource(ctx, tx, source); err != nil {
			return err
		}
		record := core.MemoryRecord{ID: fmt.Sprintf("import_%d", i.id), Content: i.content, Kind: "thought", State: "open", Basis: "unverified", Revision: 1, RecordedAt: created, UpdatedAt: updated, SourceIDs: []string{source.ID}, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: i.content, Relation: "supports"}}, Entities: []string{}, Parents: []string{}}
		if i.kind == "said" {
			record.Kind = "fact"
			record.State = "current"
		}
		if i.kind == "said" && i.run != "" {
			var message core.MemorySource
			var createdAt string
			err := tx.QueryRowContext(ctx, `SELECT CAST(id AS TEXT),session_id,run_id,created_at FROM session_messages WHERE run_id=? AND role IN ('user','capture') AND instr(content,?)>0 ORDER BY id LIMIT 1`, i.run, i.content).Scan(&message.ObjectID, &message.SessionID, &message.RunID, &createdAt)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				message.ID = "message:" + message.ObjectID
				message.Kind = "message"
				message.Version = "1"
				message.Speaker = "owner"
				message.RecordedAt, err = time.Parse(time.RFC3339Nano, createdAt)
				if err != nil {
					return err
				}
				if err := registerMemorySource(ctx, tx, message); err != nil {
					return err
				}
				record.Basis = "direct"
				record.SourceIDs = []string{message.ID}
				record.Evidence = []core.MemoryEvidence{{SourceID: message.ID, Quote: i.content, Relation: "supports"}}
			}
		}
		if i.kind == "tendency" || i.kind == "ruler" {
			record.Kind = "insight"
			record.State = "disputed"
			record.NeedsReview = true
		}
		if i.state == "released" || i.state == "internalized" {
			if record.Kind == "thought" {
				record.State = "released"
			} else {
				record.State = "retracted"
			}
		}
		if err := persistMemoryRecord(ctx, tx, record, "imported working memory"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_evidence(record_id,source_id,quote,relation) VALUES(?,?,?,'supports')`, record.ID, record.Evidence[0].SourceID, i.content); err != nil {
			return err
		}
	}
	return nil
}

func migrateCommitment(ctx context.Context, tx *sql.Tx, i importedMemory) error {
	state := "scheduled"
	switch i.state {
	case "woken":
		state = "due"
	case "settled":
		state = "completed"
	case "released":
		state = "cancelled"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commitments(id,content,state,session_id,source_run_id,wake_at,recurrence,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, i.id, i.content, state, i.session, i.run, i.wake, i.recurrence, i.created, i.updated); err != nil {
		return err
	}
	if state == "scheduled" {
		return nil
	}
	var runID string
	rows, err := tx.QueryContext(ctx, `SELECT run_id,payload_json FROM events WHERE kind='wake.fired' ORDER BY sequence`)
	if err != nil {
		return err
	}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id, data string
		if err := scan(&id, &data); err != nil {
			return err
		}
		var payload struct {
			CommitmentID int64 `json:"memory_id"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return err
		}
		if payload.CommitmentID == i.id {
			runID = id
		}
		return nil
	}); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO commitment_occurrences(commitment_id,due_at,state,run_id,updated_at) VALUES(?,?,?,?,?)`, i.id, i.wake, state, runID, i.updated)
	return err
}
