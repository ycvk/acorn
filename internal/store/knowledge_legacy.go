package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// COMPAT: databases written before notes moved into SQLite — remove after the
// deployed installation has started once on this schema.

// dropLegacyKnowledgeIndex removes the file-backed knowledge index and the
// tables that only served it, before the current schema is created.
func (s *Store) dropLegacyKnowledgeIndex() error {
	ctx := context.Background()
	columns, err := s.tableColumns("knowledge_notes")
	if err != nil {
		return err
	}
	if _, legacy := columns["mtime_ns"]; !legacy {
		return nil
	}
	for _, statement := range []string{
		`DROP TABLE knowledge_notes`,
		`DROP TABLE IF EXISTS knowledge_notes_fts`,
		`DROP TABLE IF EXISTS memory_source_cursors`,
		`DROP TABLE IF EXISTS schema_migrations`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("drop legacy knowledge index: %w", err)
		}
	}
	return nil
}

// ImportLegacyKnowledge stores notes read from the former git knowledge base as
// their first revision. Their memory sources named a commit; each path had one
// committed version, which becomes revision 1 under the same source ID.
func (s *Store) ImportLegacyKnowledge(ctx context.Context, notes []core.KnowledgeNote) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		paths := map[string]bool{}
		for _, note := range notes {
			paths[note.Path] = true
			tags := strings.Join(note.Tags, " ")
			result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_notes (path, title, tags, source, body, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
				note.Path, note.Title, tags, note.Source, note.Body, formatTimestamp(note.CreatedAt), formatTimestamp(note.UpdatedAt))
			if err != nil {
				return err
			}
			inserted, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if inserted == 0 {
				continue
			}
			sum := sha256.Sum256([]byte(note.Body))
			if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_revisions (path, revision, title, tags, source, body, body_sha256, run_id, at) VALUES (?, 1, ?, ?, ?, ?, ?, '', ?)`,
				note.Path, note.Title, tags, note.Source, note.Body, hex.EncodeToString(sum[:]), formatTimestamp(note.UpdatedAt)); err != nil {
				return err
			}
			if err := indexKnowledgeNote(ctx, tx, note.Path, note.Title, tags, note.Body); err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT object_id, COUNT(*) FROM memory_sources WHERE kind='knowledge' AND version<>'1' GROUP BY object_id`)
		if err != nil {
			return err
		}
		versions := map[string]int{}
		if err := scanRows(rows, func(scan func(...any) error) error {
			var path string
			var n int
			if err := scan(&path, &n); err != nil {
				return err
			}
			versions[path] = n
			return nil
		}); err != nil {
			return err
		}
		for path, n := range versions {
			if !paths[path] {
				return fmt.Errorf("import legacy knowledge: memory source names %s, which is no longer in the knowledge base", path)
			}
			if n > 1 {
				return fmt.Errorf("import legacy knowledge: %s has %d committed versions; only the current one can be imported", path, n)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE memory_sources SET version='1' WHERE kind='knowledge' AND object_id=?`, path); err != nil {
				return err
			}
		}
		return nil
	})
}
