package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// knowledgeSnippetRunes bounds the body preview of a listed note.
const knowledgeSnippetRunes = 160

// WriteKnowledgeNote stores a new revision of a note and registers it as a
// memory source in the same transaction.
func (s *Store) WriteKnowledgeNote(ctx context.Context, w core.KnowledgeWrite) (core.KnowledgeNote, error) {
	if strings.TrimSpace(w.Path) == "" || strings.TrimSpace(w.Title) == "" || w.At.IsZero() {
		return core.KnowledgeNote{}, errors.New("write knowledge note: path, title and at are required")
	}
	var note core.KnowledgeNote
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		current, err := loadKnowledgeNote(ctx, tx, w.Path)
		exists := err == nil
		if err != nil && !errors.Is(err, core.ErrKnowledgeNoteNotFound) {
			return err
		}
		if w.BaseRevision > 0 && current.Revision != w.BaseRevision {
			return fmt.Errorf("%w: %s is at revision %d, not %d", core.ErrKnowledgeConflict, w.Path, current.Revision, w.BaseRevision)
		}
		note = core.KnowledgeNote{Path: w.Path, Title: w.Title, Tags: w.Tags, Source: w.Source, Body: w.Body, Revision: current.Revision + 1, CreatedAt: w.At, UpdatedAt: w.At}
		if exists {
			note.CreatedAt = current.CreatedAt
		}
		tags := strings.Join(note.Tags, " ")
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_notes (path, title, tags, source, body, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET title = excluded.title, tags = excluded.tags, source = excluded.source, body = excluded.body, revision = excluded.revision, updated_at = excluded.updated_at`,
			note.Path, note.Title, tags, note.Source, note.Body, note.Revision, formatTimestamp(note.CreatedAt), formatTimestamp(note.UpdatedAt)); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(note.Body))
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_revisions (path, revision, title, tags, source, body, body_sha256, run_id, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			note.Path, note.Revision, note.Title, tags, note.Source, note.Body, hex.EncodeToString(sum[:]), w.RunID, formatTimestamp(w.At)); err != nil {
			return err
		}
		if err := indexKnowledgeNote(ctx, tx, note.Path, note.Title, tags, note.Body); err != nil {
			return err
		}
		speaker := "external"
		if w.RunID != "" {
			speaker = "assistant"
		}
		return registerMemorySource(ctx, tx, core.MemorySource{ID: core.KnowledgeSourceID(note.Path, note.Revision), Kind: "knowledge", ObjectID: note.Path, Version: strconv.FormatInt(note.Revision, 10), Speaker: speaker, RunID: w.RunID, RecordedAt: w.At, OccurredAt: w.At})
	})
	if err != nil {
		return core.KnowledgeNote{}, fmt.Errorf("write knowledge note %s: %w", w.Path, err)
	}
	return note, nil
}

func indexKnowledgeNote(ctx context.Context, tx *sql.Tx, path, title, tags, body string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_notes_fts WHERE path = ?`, path); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_notes_fts (path, title, tags, body) VALUES (?, ?, ?, ?)`, path, title, tags, body)
	return err
}

func (s *Store) KnowledgeNote(ctx context.Context, path string) (core.KnowledgeNote, error) {
	return loadKnowledgeNote(ctx, s.db, path)
}

func loadKnowledgeNote(ctx context.Context, q memorySQL, path string) (core.KnowledgeNote, error) {
	note := core.KnowledgeNote{Path: path}
	var tags, created, updated string
	err := q.QueryRowContext(ctx, `SELECT title, tags, source, body, revision, created_at, updated_at FROM knowledge_notes WHERE path = ?`, path).
		Scan(&note.Title, &tags, &note.Source, &note.Body, &note.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return core.KnowledgeNote{}, fmt.Errorf("%w: %s", core.ErrKnowledgeNoteNotFound, path)
	}
	if err != nil {
		return core.KnowledgeNote{}, fmt.Errorf("read knowledge note %s: %w", path, err)
	}
	note.Tags = strings.Fields(tags)
	if note.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "knowledge_notes.created_at"); err != nil {
		return core.KnowledgeNote{}, err
	}
	if note.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "knowledge_notes.updated_at"); err != nil {
		return core.KnowledgeNote{}, err
	}
	return note, nil
}

func (s *Store) CountKnowledgeNotes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_notes`).Scan(&n)
	return n, err
}

func (s *Store) SearchKnowledge(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search knowledge: query is required")
	}
	if limit <= 0 {
		return nil, errors.New("search knowledge: limit must be positive")
	}
	if utf8.RuneCountInString(query) >= trigramMinRunes {
		match := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		rows, err := s.db.QueryContext(ctx,
			`SELECT n.path, n.title, n.tags, snippet(knowledge_notes_fts, 3, '', '', '…', 16), n.revision, n.updated_at
			 FROM knowledge_notes_fts JOIN knowledge_notes n ON n.path = knowledge_notes_fts.path
			 WHERE knowledge_notes_fts MATCH ? ORDER BY bm25(knowledge_notes_fts) LIMIT ?`, match, limit)
		if err != nil {
			return nil, fmt.Errorf("search knowledge: %w", err)
		}
		return scanKnowledgeHits(rows, func(snippet string) string { return snippet })
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, title, tags, body, revision, updated_at FROM knowledge_notes
		 WHERE title LIKE ? ESCAPE '\' OR tags LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\'
		 ORDER BY updated_at DESC LIMIT ?`, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	return scanKnowledgeHits(rows, func(body string) string { return snippetAround(body, query) })
}

func (s *Store) RecentKnowledge(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	if limit <= 0 {
		return nil, errors.New("recent knowledge: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, title, tags, body, revision, updated_at FROM knowledge_notes
		 WHERE path LIKE ? ESCAPE '\' ORDER BY updated_at DESC, path ASC LIMIT ?`,
		escapeLike(prefix)+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("recent knowledge: %w", err)
	}
	return scanKnowledgeHits(rows, func(body string) string { return leadingRunes(body, knowledgeSnippetRunes) })
}

// scanKnowledgeHits reads (path, title, tags, text, revision, updated_at) rows; snippet
// turns the text column into the hit's snippet.
func scanKnowledgeHits(rows *sql.Rows, snippet func(text string) string) ([]core.KnowledgeHit, error) {
	defer func() { _ = rows.Close() }()
	hits := []core.KnowledgeHit{}
	for rows.Next() {
		var (
			hit              core.KnowledgeHit
			tags, text, when string
		)
		if err := rows.Scan(&hit.Path, &hit.Title, &tags, &text, &hit.Revision, &when); err != nil {
			return nil, fmt.Errorf("scan knowledge hit: %w", err)
		}
		hit.Tags = strings.Fields(tags)
		hit.Snippet = collapseSpace(snippet(text))
		updated, err := parseTimestamp(fixedTimestampLayout, when, "knowledge_notes.updated_at")
		if err != nil {
			return nil, err
		}
		hit.UpdatedAt = updated
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan knowledge hits: %w", err)
	}
	return hits, nil
}

// collapseSpace joins a snippet onto one line so list previews read as text.
func collapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func leadingRunes(text string, n int) string {
	text = collapseSpace(text)
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}
