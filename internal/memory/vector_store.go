package memory

import "context"

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
