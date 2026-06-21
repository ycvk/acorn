package memorymodule

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// VectorStore stores embedding vectors keyed by memory record ref and searches
// them by cosine similarity. The pure-Go brute-force search is fine for the
// expected scale (<10k records).
type VectorStore interface {
	Store(ctx context.Context, ref string, kind Kind, contentHash string, vector []float32, model string, dimensions int) error
	Search(ctx context.Context, queryVector []float32, limit int) ([]VectorSearchResult, error)
	Delete(ctx context.Context, ref string) error
}

// VectorSearchResult is a single cosine-similarity hit.
type VectorSearchResult struct {
	Ref   string
	Kind  Kind
	Score float64
}

// SQLiteVectorStore implements VectorStore over a SQLite table named
// memory_vectors. The table is created lazily on first use; callers that share
// the database should add the DDL to their schema bootstrap instead.
type SQLiteVectorStore struct {
	db *sql.DB
}

// NewSQLiteVectorStore wraps an already-open SQLite database. The
// memory_vectors table is created if it does not exist.
func NewSQLiteVectorStore(db *sql.DB) (*SQLiteVectorStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite vector store requires a database handle")
	}
	store := &SQLiteVectorStore{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

const memoryVectorsSchema = `
CREATE TABLE IF NOT EXISTS memory_vectors (
    ref TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    vector_blob BLOB NOT NULL,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`

func (s *SQLiteVectorStore) ensureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, memoryVectorsSchema)
	if err != nil {
		return fmt.Errorf("create memory_vectors table: %w", err)
	}
	return nil
}

// Store upserts a vector for ref. A non-matching content_hash skips re-writing
// only when the caller checks it first; Store itself always writes the vector.
func (s *SQLiteVectorStore) Store(ctx context.Context, ref string, kind Kind, contentHash string, vector []float32, model string, dimensions int) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("vector store ref is required")
	}
	if len(vector) != dimensions {
		return fmt.Errorf("vector store dimensions = %d, want %d", len(vector), dimensions)
	}
	blob, err := encodeFloat32Vector(vector)
	if err != nil {
		return fmt.Errorf("encode vector %q: %w", ref, err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO memory_vectors (ref, kind, content_hash, vector_blob, model, dimensions)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(ref) DO UPDATE SET
    kind = excluded.kind,
    content_hash = excluded.content_hash,
    vector_blob = excluded.vector_blob,
    model = excluded.model,
    dimensions = excluded.dimensions,
    created_at = datetime('now')
`, ref, string(kind), strings.TrimSpace(contentHash), blob, strings.TrimSpace(model), dimensions)
	if err != nil {
		return fmt.Errorf("upsert vector %q: %w", ref, err)
	}
	return nil
}

func (s *SQLiteVectorStore) Search(ctx context.Context, queryVector []float32, limit int) ([]VectorSearchResult, error) {
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("query vector is required")
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ref, kind, vector_blob, dimensions FROM memory_vectors`)
	if err != nil {
		return nil, fmt.Errorf("query memory vectors: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		ref   string
		kind  Kind
		score float64
	}
	var hits []candidate
	for rows.Next() {
		var ref string
		var kind string
		var blob []byte
		var dims int
		if err := rows.Scan(&ref, &kind, &blob, &dims); err != nil {
			return nil, fmt.Errorf("scan memory vector: %w", err)
		}
		vec, err := decodeFloat32Vector(blob, dims)
		if err != nil {
			return nil, fmt.Errorf("decode vector %q: %w", ref, err)
		}
		score := cosineSimilarity(queryVector, vec)
		hits = append(hits, candidate{ref: ref, kind: Kind(kind), score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory vectors: %w", err)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].ref < hits[j].ref
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]VectorSearchResult, len(hits))
	for i, h := range hits {
		out[i] = VectorSearchResult{Ref: h.ref, Kind: h.kind, Score: h.score}
	}
	return out, nil
}

func (s *SQLiteVectorStore) Delete(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("vector store delete ref is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory_vectors WHERE ref = ?`, ref)
	if err != nil {
		return fmt.Errorf("delete vector %q: %w", ref, err)
	}
	return nil
}

// encodeFloat32Vector serializes a float32 slice as little-endian bytes.
func encodeFloat32Vector(vec []float32) ([]byte, error) {
	if len(vec) == 0 {
		return nil, nil
	}
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[4*i:4*i+4], math.Float32bits(v))
	}
	return buf, nil
}

// decodeFloat32Vector deserializes a little-endian float32 byte slice.
func decodeFloat32Vector(buf []byte, dims int) ([]float32, error) {
	if len(buf) == 0 {
		return nil, fmt.Errorf("empty vector blob")
	}
	if len(buf) != 4*dims {
		return nil, fmt.Errorf("vector blob length = %d, want %d", len(buf), 4*dims)
	}
	vec := make([]float32, dims)
	for i := 0; i < dims; i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i : 4*i+4]))
	}
	return vec, nil
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 for zero-length or zero-norm vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
