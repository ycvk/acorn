package memory

import (
	"context"
	"testing"
)

// TestFuseResultsKeywordOnly verifies that when vector results are absent
// (embedding not configured), fuseResults returns keyword results unchanged.
func TestFuseResultsKeywordOnly(t *testing.T) {
	keyword := []SearchItem{
		{Ref: "a", Score: 5},
		{Ref: "b", Score: 3},
	}
	result := fuseResults(keyword, nil, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].Ref != "a" || result[0].Score != 5 {
		t.Fatalf("expected a:5, got %s:%v", result[0].Ref, result[0].Score)
	}
}

// TestFuseResultsVectorOnly verifies that when keyword results are absent,
// fuseResults returns vector results unchanged.
func TestFuseResultsVectorOnly(t *testing.T) {
	vector := []SearchItem{
		{Ref: "x", Score: 0.9},
		{Ref: "y", Score: 0.7},
	}
	result := fuseResults(nil, vector, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].Ref != "x" {
		t.Fatalf("expected x first, got %s", result[0].Ref)
	}
}

// TestFuseResultsBothLists verifies RRF fusion: an item in both lists should
// rank higher than an item in only one list, even if the single-list item has
// a higher raw score.
func TestFuseResultsBothLists(t *testing.T) {
	keyword := []SearchItem{
		{Ref: "a", Score: 5}, // rank 0 in keyword
		{Ref: "b", Score: 3}, // rank 1 in keyword
	}
	vector := []SearchItem{
		{Ref: "b", Score: 0.9}, // rank 0 in vector
		{Ref: "c", Score: 0.7}, // rank 1 in vector
	}
	result := fuseResults(keyword, vector, 10)
	if len(result) != 3 {
		t.Fatalf("expected 3 fused results, got %d", len(result))
	}
	// "b" appears in both lists: RRF = 1/(60+1) + 1/(60+2) ≈ 0.0328
	// "a" only keyword rank 0: RRF = 1/(60+1) ≈ 0.0164
	// "c" only vector rank 1: RRF = 1/(60+2) ≈ 0.0161
	// So b > a > c.
	if result[0].Ref != "b" {
		t.Fatalf("expected b first (in both lists), got %s", result[0].Ref)
	}
	if result[1].Ref != "a" {
		t.Fatalf("expected a second, got %s", result[1].Ref)
	}
	if result[2].Ref != "c" {
		t.Fatalf("expected c third, got %s", result[2].Ref)
	}
}

// TestFuseResultsLimit verifies that the limit is respected after fusion.
func TestFuseResultsLimit(t *testing.T) {
	keyword := make([]SearchItem, 5)
	vector := make([]SearchItem, 5)
	for i := 0; i < 5; i++ {
		keyword[i] = SearchItem{Ref: string(rune('a' + i)), Score: float64(5 - i)}
		vector[i] = SearchItem{Ref: string(rune('a' + i + 5)), Score: float64(5-i) * 0.1}
	}
	result := fuseResults(keyword, vector, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 results after limit, got %d", len(result))
	}
}

// TestVectorIndexCRUD verifies the sqlite-vec-backed VectorIndex can create,
// search, and delete embeddings.
func TestVectorIndexCRUD(t *testing.T) {
	vi, err := NewVectorIndex(t.TempDir(), 3)
	if err != nil {
		t.Fatalf("NewVectorIndex: %v", err)
	}
	defer vi.Close()
	ctx := context.Background()

	// Insert two embeddings.
	if err := vi.UpsertEmbedding(ctx, "fact:alpha", "fact", "Alpha", "user",
		[]float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("UpsertEmbedding alpha: %v", err)
	}
	if err := vi.UpsertEmbedding(ctx, "fact:beta", "fact", "Beta", "user",
		[]float32{0.9, 0.8, 0.7}); err != nil {
		t.Fatalf("UpsertEmbedding beta: %v", err)
	}

	// Search with a vector close to alpha.
	matches, err := vi.SearchByVector(ctx, []float32{0.1, 0.2, 0.3}, 2, nil, "")
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].Ref != "fact:alpha" {
		t.Fatalf("expected alpha first, got %s (distance=%.4f)", matches[0].Ref, matches[0].Score)
	}

	// Delete alpha and verify it's gone.
	if err := vi.DeleteByRef(ctx, "fact:alpha"); err != nil {
		t.Fatalf("DeleteByRef: %v", err)
	}
	matches, err = vi.SearchByVector(ctx, []float32{0.1, 0.2, 0.3}, 2, nil, "")
	if err != nil {
		t.Fatalf("SearchByVector after delete: %v", err)
	}
	if len(matches) != 1 || matches[0].Ref != "fact:beta" {
		t.Fatalf("expected only beta after delete, got %d matches", len(matches))
	}
}

// TestSearchDegradesToKeywordWhenEmbeddingDisabled verifies that a
// LocalService without an EmbeddingClient still produces keyword results
// (the pre-existing behavior) and does not attempt vector search.
func TestSearchDegradesToKeywordWhenEmbeddingDisabled(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	// Write a fact that should be findable by keyword.
	if _, err := service.CreateFact(ctx, CreateFactRequest{
		Title: "Acorn test fact",
		Body:  "The quick brown fox jumps over the lazy dog",
		Tags:  []string{"test"},
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}

	// Search without embedding configured.
	result, err := service.Search(ctx, SearchRequest{
		Query: "quick brown fox",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected keyword search results, got none")
	}
	found := false
	for _, item := range result.Items {
		if item.Title == "Acorn test fact" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'Acorn test fact' in keyword results")
	}

	// Verify no vector stage in explain (embedding is nil).
	result, _ = service.Search(ctx, SearchRequest{Query: "fox", Limit: 5, Explain: true})
	if result.Explain != nil {
		for _, stage := range result.Explain.Stages {
			if stage.Name == searchStageVector {
				t.Fatal("vector stage should not appear when embedding is disabled")
			}
		}
	}
}

// TestVectorIndexUpsertReplaces verifies that upserting the same ref replaces
// the old embedding rather than creating a duplicate.
func TestVectorIndexUpsertReplaces(t *testing.T) {
	vi, err := NewVectorIndex(t.TempDir(), 3)
	if err != nil {
		t.Fatalf("NewVectorIndex: %v", err)
	}
	defer vi.Close()
	ctx := context.Background()

	// Insert original.
	if err := vi.UpsertEmbedding(ctx, "ref:same", "fact", "Title", "user",
		[]float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}
	// Upsert replacement (different vector).
	if err := vi.UpsertEmbedding(ctx, "ref:same", "fact", "Updated Title", "user",
		[]float32{0.9, 0.8, 0.7}); err != nil {
		t.Fatalf("UpsertEmbedding replace: %v", err)
	}

	count, err := vi.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 vector after replace, got %d", count)
	}

	// Search with the new vector — should match.
	matches, err := vi.SearchByVector(ctx, []float32{0.9, 0.8, 0.7}, 1, nil, "")
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(matches) != 1 || matches[0].Ref != "ref:same" {
		t.Fatalf("expected ref:same, got %d: %+v", len(matches), matches)
	}
	if matches[0].Title != "Updated Title" {
		t.Fatalf("expected updated title, got %q", matches[0].Title)
	}
}
