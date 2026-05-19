//go:build bleve_faiss && vectors && cgo

package memorymodule

import (
	"strings"
	"testing"
)

func TestBleveSemanticIndexRebuildAndSearch(t *testing.T) {
	index, err := NewBleveSemanticIndex(t.Context(), BleveSemanticIndexConfig{
		Path:       t.TempDir(),
		IndexName:  "memory_records",
		Dimensions: 3,
	})
	if err != nil {
		t.Fatalf("NewBleveSemanticIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	if _, err := index.Rebuild(t.Context(), SemanticRebuildRequest{
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
		Records: []IndexedSemanticRecord{
			{
				Record: SemanticRecord{
					Ref:         "facts/workspaces/acorn.md#acorn",
					Kind:        KindFact,
					Scope:       "workspace:acorn",
					Status:      StatusVerified,
					Title:       "Acorn",
					Body:        "Acorn uses Bleve + FAISS semantic retrieval.",
					Path:        "facts/workspaces/acorn.md",
					ContentHash: "hash-a",
				},
				Vector: []float32{0.1, 0.2, 0.3},
			},
			{
				Record: SemanticRecord{
					Ref:         "skills/learned/procedure.md#procedure",
					Kind:        KindSkill,
					Scope:       "workspace:acorn",
					Status:      StatusVerified,
					Title:       "Procedure",
					Body:        "Procedure memory.",
					Path:        "skills/learned/procedure.md",
					ContentHash: "hash-b",
				},
				Vector: []float32{0.8, 0.7, 0.6},
			},
		},
	}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	result, err := index.Search(t.Context(), SemanticSearchRequest{
		Vector:     []float32{0.1, 0.2, 0.3},
		Dimensions: 3,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Hits) == 0 {
		t.Fatal("expected semantic hits")
	}
	if result.Hits[0].Ref == "" || result.Hits[0].Stage != searchStageSemanticVector {
		t.Fatalf("unexpected hit: %#v", result.Hits[0])
	}
}

func TestBleveSemanticIndexSearchRejectsStaleModel(t *testing.T) {
	index, err := NewBleveSemanticIndex(t.Context(), BleveSemanticIndexConfig{
		Path:       t.TempDir(),
		IndexName:  "memory_records",
		Dimensions: 3,
	})
	if err != nil {
		t.Fatalf("NewBleveSemanticIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	if _, err := index.Rebuild(t.Context(), SemanticRebuildRequest{
		Model:      "old-model",
		Dimensions: 3,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
		Records: []IndexedSemanticRecord{{
			Record: SemanticRecord{
				Ref:         "facts/workspaces/acorn.md#acorn",
				Kind:        KindFact,
				Status:      StatusVerified,
				Title:       "Acorn",
				ContentHash: "hash-a",
			},
			Vector: []float32{0.1, 0.2, 0.3},
		}},
	}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	_, err = index.Search(t.Context(), SemanticSearchRequest{
		Vector:     []float32{0.1, 0.2, 0.3},
		Model:      "new-model",
		Dimensions: 3,
		Limit:      1,
	})
	if err == nil || !strings.Contains(err.Error(), "rebuild semantic index") {
		t.Fatalf("Search error = %v, want stale model rebuild error", err)
	}
}
