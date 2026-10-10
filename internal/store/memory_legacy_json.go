package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// COMPAT: memory records and revisions stored as JSON documents — remove after
// the deployed installation has started once on this schema.

// migrateMemoryJSON moves each record and revision document into columns and
// per-revision relation rows, then drops the documents.
func (s *Store) migrateMemoryJSON(ctx context.Context) error {
	columns, err := s.tableColumns("memory_revisions")
	if err != nil {
		return err
	}
	if _, legacy := columns["data"]; !legacy {
		return nil
	}
	err = s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		for _, statement := range []string{
			`ALTER TABLE memory_records ADD COLUMN basis TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_records ADD COLUMN needs_review INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE memory_records ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE memory_revisions ADD COLUMN kind TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN content TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN scope TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN state TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN basis TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN valid_from TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN valid_to TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE memory_revisions ADD COLUMN needs_review INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE memory_revisions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		revisions, err := legacyMemoryDocuments(ctx, tx, `SELECT record_id,revision,data FROM memory_revisions ORDER BY record_id,revision`)
		if err != nil {
			return err
		}
		for _, r := range revisions {
			if _, err := tx.ExecContext(ctx, `UPDATE memory_revisions SET kind=?,content=?,scope=?,state=?,basis=?,valid_from=?,valid_to=?,needs_review=?,pinned=? WHERE record_id=? AND revision=?`,
				r.Kind, r.Content, r.Scope, r.State, r.Basis, formatZeroableTimestamp(r.ValidFrom), formatZeroableTimestamp(r.ValidTo), r.NeedsReview, r.Pinned, r.ID, r.Revision); err != nil {
				return err
			}
			for _, e := range r.Evidence {
				if _, err := tx.ExecContext(ctx, `INSERT INTO memory_revision_evidence(record_id,revision,source_id,quote,relation) VALUES(?,?,?,?,?)`, r.ID, r.Revision, e.SourceID, e.Quote, e.Relation); err != nil {
					return err
				}
			}
			for _, rel := range []struct {
				table, column string
				values        []string
			}{
				{"memory_revision_entities", "entity", r.Entities},
				{"memory_revision_parents", "parent_id", r.Parents},
				{"memory_revision_sources", "source_id", r.SourceIDs},
			} {
				for _, value := range rel.values {
					if _, err := tx.ExecContext(ctx, `INSERT INTO `+rel.table+`(record_id,revision,`+rel.column+`) VALUES(?,?,?)`, r.ID, r.Revision, value); err != nil {
						return err
					}
				}
			}
		}
		records, err := legacyMemoryDocuments(ctx, tx, `SELECT id,revision,data FROM memory_records`)
		if err != nil {
			return err
		}
		for _, r := range records {
			if _, err := tx.ExecContext(ctx, `UPDATE memory_records SET basis=?,needs_review=?,pinned=? WHERE id=?`, r.Basis, r.NeedsReview, r.Pinned, r.ID); err != nil {
				return err
			}
		}
		for _, statement := range []string{
			`ALTER TABLE memory_records DROP COLUMN data`,
			`ALTER TABLE memory_revisions DROP COLUMN data`,
			`DROP TABLE IF EXISTS memory_evidence`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("migrate memory documents: %w", err)
	}
	return nil
}

// legacyMemoryDocuments reads (id, revision, document) rows; the row's key
// wins over the document's.
func legacyMemoryDocuments(ctx context.Context, tx *sql.Tx, query string) ([]core.MemoryRecord, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var out []core.MemoryRecord
	err = scanRows(rows, func(scan func(...any) error) error {
		var id, data string
		var revision int64
		if err := scan(&id, &revision, &data); err != nil {
			return err
		}
		var r core.MemoryRecord
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return fmt.Errorf("memory %s@%d: %w", id, revision, err)
		}
		r.ID, r.Revision = id, revision
		out = append(out, r)
		return nil
	})
	return out, err
}
