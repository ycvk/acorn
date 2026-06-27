package memory

import (
	"context"
	"fmt"
)

// ReindexResult reports the outcome of a reindex pass.
type ReindexResult struct {
	Total   int
	Indexed int
	Skipped int
	Failed  int
}

// ReindexEmbeddings regenerates embeddings for all file-backed memory records.
// It is the one-shot path to populate the vector index for pre-existing records
// after embedding is first enabled — the write path only embeds new/updated
// records, so old data would otherwise remain invisible to semantic search.
//
// When embedding is not configured (no EmbeddingClient or VectorIndex), the
// method returns an error: calling reindex without embedding enabled is a
// usage error, not a silent no-op.
func (s *LocalService) ReindexEmbeddings(ctx context.Context) (*ReindexResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if s.embedding == nil || s.vectors == nil {
		return nil, fmt.Errorf("embedding is not enabled — set memory.embedding.enabled in config and restart")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	if err := s.BuildIndex(ctx); err != nil {
		return nil, fmt.Errorf("build index before reindex: %w", err)
	}

	// Collect all records including retired ones so stale embeddings are
	// overwritten rather than left behind.
	selection := RecordSelection{IncludeRetired: true, IncludeInactive: true}
	result := &ReindexResult{}

	collectAndReindex := func(kind Kind) error {
		var records []Record
		var err error
		switch kind {
		case KindFact:
			records, err = s.ListFacts(ctx, selection)
		case KindSkill:
			records, err = s.ListSkills(ctx, selection)
		case KindHistory:
			records, err = s.ListHistory(ctx, selection)
		}
		if err != nil {
			return fmt.Errorf("list %s records: %w", kind, err)
		}
		for _, record := range records {
			result.Total++
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			switch s.indexEmbedding(ctx, record) {
			case embedIndexed:
				result.Indexed++
			case embedFailed:
				result.Failed++
			case embedSkipped:
				result.Skipped++
			}
		}
		return nil
	}

	for _, kind := range []Kind{KindFact, KindSkill, KindHistory} {
		if err := collectAndReindex(kind); err != nil {
			return result, err
		}
	}
	return result, nil
}
