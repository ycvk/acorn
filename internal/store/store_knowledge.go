package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// knowledgeSnippetRunes bounds the body preview of a listed note.
const knowledgeSnippetRunes = 160

// UpsertKnowledgeNote replaces the index entry of one note, full-text row
// included.
func (s *Store) UpsertKnowledgeNote(ctx context.Context, note core.KnowledgeNote) (err error) {
	if strings.TrimSpace(note.Path) == "" {
		return errors.New("upsert knowledge note: path is required")
	}
	if note.UpdatedAt.IsZero() {
		return errors.New("upsert knowledge note: updated_at is required")
	}
	tags := strings.Join(note.Tags, " ")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsert knowledge note %s: %w", note.Path, err)
	}
	defer rollbackOnErr(tx, &err, "upsert knowledge note")
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO knowledge_notes (path, title, tags, body, mtime_ns, size, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET title = excluded.title, tags = excluded.tags, body = excluded.body,
		   mtime_ns = excluded.mtime_ns, size = excluded.size, updated_at = excluded.updated_at`,
		note.Path, note.Title, tags, note.Body, note.MTimeNS, note.Size, formatTimestamp(note.UpdatedAt)); err != nil {
		return fmt.Errorf("upsert knowledge note %s: %w", note.Path, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_notes_fts WHERE path = ?`, note.Path); err != nil {
		return fmt.Errorf("upsert knowledge note %s: %w", note.Path, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO knowledge_notes_fts (path, title, tags, body) VALUES (?, ?, ?, ?)`,
		note.Path, note.Title, tags, note.Body); err != nil {
		return fmt.Errorf("upsert knowledge note %s: %w", note.Path, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("upsert knowledge note %s: %w", note.Path, err)
	}
	return nil
}

// DeleteKnowledgeNote removes a note from the index. Deleting a path that is
// not indexed is not an error.
func (s *Store) DeleteKnowledgeNote(ctx context.Context, path string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete knowledge note %s: %w", path, err)
	}
	defer rollbackOnErr(tx, &err, "delete knowledge note")
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_notes WHERE path = ?`, path); err != nil {
		return fmt.Errorf("delete knowledge note %s: %w", path, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_notes_fts WHERE path = ?`, path); err != nil {
		return fmt.Errorf("delete knowledge note %s: %w", path, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete knowledge note %s: %w", path, err)
	}
	return nil
}

func (s *Store) ListKnowledgeFileStats(ctx context.Context) ([]core.KnowledgeFileStat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, mtime_ns, size FROM knowledge_notes`)
	if err != nil {
		return nil, fmt.Errorf("list knowledge file stats: %w", err)
	}
	var stats []core.KnowledgeFileStat
	if err := scanRows(rows, func(scan func(...any) error) error {
		var st core.KnowledgeFileStat
		if err := scan(&st.Path, &st.MTimeNS, &st.Size); err != nil {
			return err
		}
		stats = append(stats, st)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list knowledge file stats: %w", err)
	}
	return stats, nil
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
			`SELECT n.path, n.title, n.tags, snippet(knowledge_notes_fts, 3, '', '', '…', 16), n.updated_at
			 FROM knowledge_notes_fts JOIN knowledge_notes n ON n.path = knowledge_notes_fts.path
			 WHERE knowledge_notes_fts MATCH ? ORDER BY bm25(knowledge_notes_fts) LIMIT ?`, match, limit)
		if err != nil {
			return nil, fmt.Errorf("search knowledge: %w", err)
		}
		return scanKnowledgeHits(rows, func(snippet string) string { return snippet })
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, title, tags, body, updated_at FROM knowledge_notes
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
		`SELECT path, title, tags, body, updated_at FROM knowledge_notes
		 WHERE path LIKE ? ESCAPE '\' ORDER BY updated_at DESC, path ASC LIMIT ?`,
		escapeLike(prefix)+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("recent knowledge: %w", err)
	}
	return scanKnowledgeHits(rows, func(body string) string { return leadingRunes(body, knowledgeSnippetRunes) })
}

// scanKnowledgeHits reads (path, title, tags, text, updated_at) rows; snippet
// turns the text column into the hit's snippet.
func scanKnowledgeHits(rows *sql.Rows, snippet func(text string) string) ([]core.KnowledgeHit, error) {
	defer func() { _ = rows.Close() }()
	hits := []core.KnowledgeHit{}
	for rows.Next() {
		var (
			hit              core.KnowledgeHit
			tags, text, when string
		)
		if err := rows.Scan(&hit.Path, &hit.Title, &tags, &text, &when); err != nil {
			return nil, fmt.Errorf("scan knowledge hit: %w", err)
		}
		hit.Tags = strings.Fields(tags)
		hit.Snippet = snippet(text)
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

func leadingRunes(text string, n int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}
