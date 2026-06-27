package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// VectorIndex is a sqlite-vec-backed vector store for memory semantic search.
// It lives in a dedicated SQLite file ({memory_root}/vectors.db), separate
// from the runtime acorn.db, so the memory subsystem owns its own read/write
// path without contending for the single serialized runtime connection.
//
// The vec0 virtual table stores embeddings; a shadow table stores metadata
// (ref, kind, title, scope) so KNN results can be joined back to memory
// records without a second lookup. The index is a derived artifact: it can
// be rebuilt from file-backed records at any time via Rebuild.
type VectorIndex struct {
	db   *sql.DB
	dims int
	root string
}

// VectorMatch is a single KNN result.
type VectorMatch struct {
	Ref   string
	Kind  string
	Title string
	Scope string
	Score float64 // cosine distance (lower = more similar); converted to similarity in Search
}

// NewVectorIndex opens or creates the vector index at {dir}/vectors.db.
// dims must match the embedding model's output dimension.
func NewVectorIndex(dir string, dims int) (*VectorIndex, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("vector index dir is required")
	}
	if dims <= 0 {
		return nil, errors.New("vector index dims must be > 0")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create vector index dir: %w", err)
	}
	dbPath := filepath.Join(dir, "vectors.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open vector index db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	vi := &VectorIndex{db: db, dims: dims, root: dir}
	if err := vi.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return vi, nil
}

func (vi *VectorIndex) init() error {
	// Shadow table: stores metadata alongside the vector id.
	_, err := vi.db.Exec(`CREATE TABLE IF NOT EXISTS vec_meta (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ref TEXT NOT NULL UNIQUE,
		kind TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return fmt.Errorf("create vec_meta: %w", err)
	}
	// Virtual table: vec0 stores the embedding. The id joins to vec_meta.id.
	createVec := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS vec_items USING vec0(
			id INTEGER PRIMARY KEY,
			embedding float[%d]
		)`, vi.dims)
	_, err = vi.db.Exec(createVec)
	if err != nil {
		return fmt.Errorf("create vec_items: %w", err)
	}
	_, err = vi.db.Exec(`CREATE INDEX IF NOT EXISTS idx_vec_meta_ref ON vec_meta(ref)`)
	return err
}

// Close closes the underlying database connection.
func (vi *VectorIndex) Close() error {
	if vi == nil || vi.db == nil {
		return nil
	}
	return vi.db.Close()
}

// UpsertEmbedding inserts or replaces the embedding for a given ref.
// If the ref already exists, its embedding and metadata are updated.
func (vi *VectorIndex) UpsertEmbedding(ctx context.Context, ref, kind, title, scope string, embedding []float32) error {
	if vi == nil {
		return errors.New("vector index is nil")
	}
	if len(embedding) != vi.dims {
		return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(embedding), vi.dims)
	}
	vecJSON := vectorToJSON(embedding)
	tx, err := vi.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete existing entry for this ref (if any) in both tables.
	if _, err := tx.ExecContext(ctx, `DELETE FROM vec_items WHERE id IN (SELECT id FROM vec_meta WHERE ref = ?)`, ref); err != nil {
		return fmt.Errorf("delete old vec_items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vec_meta WHERE ref = ?`, ref); err != nil {
		return fmt.Errorf("delete old vec_meta: %w", err)
	}

	// Insert metadata first to get the id.
	res, err := tx.ExecContext(ctx,
		`INSERT INTO vec_meta (ref, kind, title, scope) VALUES (?, ?, ?, ?)`,
		ref, kind, title, scope)
	if err != nil {
		return fmt.Errorf("insert vec_meta: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get vec_meta id: %w", err)
	}

	// Insert vector.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO vec_items (id, embedding) VALUES (?, vec_f32(?))`,
		id, vecJSON)
	if err != nil {
		return fmt.Errorf("insert vec_items: %w", err)
	}
	return tx.Commit()
}

// DeleteByRef removes the embedding and metadata for a given ref.
// A missing ref is a no-op.
func (vi *VectorIndex) DeleteByRef(ctx context.Context, ref string) error {
	if vi == nil {
		return errors.New("vector index is nil")
	}
	_, err := vi.db.ExecContext(ctx, `DELETE FROM vec_items WHERE id IN (SELECT id FROM vec_meta WHERE ref = ?)`, ref)
	if err != nil {
		return fmt.Errorf("delete vec_items by ref: %w", err)
	}
	_, err = vi.db.ExecContext(ctx, `DELETE FROM vec_meta WHERE ref = ?`, ref)
	return err
}

// SearchByVector performs a KNN query against the vector index, returning the
// top-k most similar records. It returns cosine distance (0 = identical).
// Results are filtered by kind and scope when non-empty.
func (vi *VectorIndex) SearchByVector(ctx context.Context, embedding []float32, limit int, kinds []string, scope string) ([]VectorMatch, error) {
	if vi == nil {
		return nil, errors.New("vector index is nil")
	}
	if len(embedding) != vi.dims {
		return nil, fmt.Errorf("query embedding dimension mismatch: got %d, want %d", len(embedding), vi.dims)
	}
	if limit <= 0 {
		limit = 10
	}
	vecJSON := vectorToJSON(embedding)

	query := `SELECT m.ref, m.kind, m.title, m.scope,
		vec_distance_cosine(v.embedding, vec_f32(?)) AS distance
		FROM vec_items v JOIN vec_meta m ON v.id = m.id
		WHERE 1=1`
	args := []any{vecJSON}
	if len(kinds) > 0 {
		placeholders := make([]string, len(kinds))
		for i, k := range kinds {
			placeholders[i] = "?"
			args = append(args, k)
		}
		query += " AND m.kind IN (" + strings.Join(placeholders, ",") + ")"
	}
	if strings.TrimSpace(scope) != "" {
		query += " AND m.scope = ?"
		args = append(args, scope)
	}
	query += " ORDER BY distance LIMIT ?"
	args = append(args, limit)

	rows, err := vi.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search query: %w", err)
	}
	defer rows.Close()

	matches := make([]VectorMatch, 0, limit)
	for rows.Next() {
		var m VectorMatch
		if err := rows.Scan(&m.Ref, &m.Kind, &m.Title, &m.Scope, &m.Score); err != nil {
			return nil, fmt.Errorf("scan vector match: %w", err)
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

// Count returns the number of indexed vectors.
func (vi *VectorIndex) Count(ctx context.Context) (int, error) {
	var n int
	err := vi.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vec_meta`).Scan(&n)
	return n, err
}

func vectorToJSON(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf("%g", f))
	}
	b.WriteByte(']')
	return b.String()
}
