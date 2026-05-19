package memorymodule

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRebuildSemanticIndexBuildsIndexedRecords(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go, lint]
status: verified
source_run: run_1
source_refs: [history/workspaces/acorn.md#acorn-dev-checks]
evidence_refs: [tool_result:1]
valid_from: 2026-05-17
created: 2026-05-17
updated: 2026-05-17
---

# Acorn Development Checks

Run make lint.
`)
	index := &capturingSemanticIndex{}
	result, err := service.RebuildSemanticIndex(t.Context(), SemanticRebuildOptions{
		Index:      index,
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		BatchSize:  2,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
	})
	if err != nil {
		t.Fatalf("RebuildSemanticIndex: %v", err)
	}
	if result.IndexedCount != 1 || result.SkippedCount != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(index.request.Records) != 1 {
		t.Fatalf("indexed records = %d", len(index.request.Records))
	}
	got := index.request.Records[0]
	if got.Record.Ref != "facts/workspaces/acorn.md#acorn-development-checks" {
		t.Fatalf("ref = %q", got.Record.Ref)
	}
	if got.Record.Path != "facts/workspaces/acorn.md" {
		t.Fatalf("path = %q", got.Record.Path)
	}
	if got.Record.SourceRun != "run_1" || got.Record.ValidFrom != "2026-05-17" {
		t.Fatalf("metadata = %#v", got.Record)
	}
	if len(got.Record.SourceRefs) != 1 || got.Record.SourceRefs[0] != "history/workspaces/acorn.md#acorn-dev-checks" {
		t.Fatalf("source refs = %#v", got.Record.SourceRefs)
	}
	if len(got.Record.EvidenceRefs) != 1 || got.Record.EvidenceRefs[0] != "tool_result:1" {
		t.Fatalf("evidence refs = %#v", got.Record.EvidenceRefs)
	}
	if got.Record.ContentHash == "" {
		t.Fatal("content hash is empty")
	}
	if len(got.Vector) != 3 {
		t.Fatalf("vector dims = %d", len(got.Vector))
	}
}

func TestRebuildSemanticIndexIndexesNonRetiredRecords(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/active.md", `---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Active Rebuild

Active record.
`)
	writeFile(t, service, "facts/workspaces/expired.md", `---
scope: workspace:acorn
tags: [go]
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
---

# Expired Rebuild

Expired but not retired record.
`)
	writeFile(t, service, "facts/workspaces/retired.md", `---
scope: workspace:acorn
tags: [go]
status: retired
created: 2026-05-17
updated: 2026-05-17
---

# Retired Rebuild

Retired record.
`)
	index := &capturingSemanticIndex{}
	if _, err := service.RebuildSemanticIndex(t.Context(), SemanticRebuildOptions{
		Index:      index,
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		BatchSize:  2,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
	}); err != nil {
		t.Fatalf("RebuildSemanticIndex: %v", err)
	}
	if len(index.request.Records) != 2 {
		t.Fatalf("indexed records = %d, want 2: %#v", len(index.request.Records), index.request.Records)
	}
	refs := []string{index.request.Records[0].Record.Ref, index.request.Records[1].Record.Ref}
	if strings.Join(refs, ",") != "facts/workspaces/active.md#active-rebuild,facts/workspaces/expired.md#expired-rebuild" {
		t.Fatalf("indexed refs = %#v", refs)
	}
}

func TestSemanticRecordContentHashIsStableAndSensitive(t *testing.T) {
	record := SemanticRecord{
		Ref:    "facts/a.md#a",
		Kind:   KindFact,
		Scope:  "workspace:acorn",
		Status: StatusVerified,
		Title:  "A",
		Body:   "body",
		Tags:   []string{"go"},
	}
	first, err := SemanticRecordContentHash(record)
	if err != nil {
		t.Fatalf("SemanticRecordContentHash: %v", err)
	}
	second, err := SemanticRecordContentHash(record)
	if err != nil {
		t.Fatalf("SemanticRecordContentHash second: %v", err)
	}
	if first != second {
		t.Fatalf("hash mismatch: %q != %q", first, second)
	}
	record.Body = "changed"
	changed, err := SemanticRecordContentHash(record)
	if err != nil {
		t.Fatalf("SemanticRecordContentHash changed: %v", err)
	}
	if changed == first {
		t.Fatal("hash did not change after body changed")
	}
}

func TestRebuildSemanticIndexAbortsOnEmbedderError(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Acorn

Body.
`)
	index := &capturingSemanticIndex{}
	_, err := service.RebuildSemanticIndex(t.Context(), SemanticRebuildOptions{
		Index:      index,
		Embedder:   failingEmbedder{err: errors.New("provider down")},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		BatchSize:  2,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
	})
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("RebuildSemanticIndex error = %v", err)
	}
	if index.called {
		t.Fatal("semantic index should not be called after embedder error")
	}
}

func TestRebuildSemanticIndexAbortsOnIndexError(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Acorn

Body.
	`)
	_, err := service.RebuildSemanticIndex(t.Context(), SemanticRebuildOptions{
		Index:      &capturingSemanticIndex{err: errors.New("bleve write failed")},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		BatchSize:  2,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
	})
	if err == nil || !strings.Contains(err.Error(), "bleve write failed") {
		t.Fatalf("RebuildSemanticIndex error = %v", err)
	}
}

type deterministicEmbedder struct {
	dimensions int
	model      string
}

func (e deterministicEmbedder) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	vectors := make([]EmbeddingVector, 0, len(req.Inputs))
	for i, input := range req.Inputs {
		values := make([]float32, e.dimensions)
		for j := range values {
			values[j] = float32(i + j + 1)
		}
		vectors = append(vectors, EmbeddingVector{Ref: input.Ref, Values: values})
	}
	return &EmbedResult{Model: e.model, Dimensions: e.dimensions, Vectors: vectors}, nil
}

type failingEmbedder struct {
	err error
}

func (e failingEmbedder) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	return nil, e.err
}

type capturingSemanticIndex struct {
	called  bool
	err     error
	request SemanticRebuildRequest
}

func (i *capturingSemanticIndex) Rebuild(ctx context.Context, req SemanticRebuildRequest) (*SemanticRebuildResult, error) {
	i.called = true
	i.request = req
	if i.err != nil {
		return nil, i.err
	}
	return &SemanticRebuildResult{
		Model:        req.Model,
		Dimensions:   req.Dimensions,
		Schema:       req.Schema,
		IndexName:    req.IndexName,
		IndexedCount: len(req.Records),
	}, nil
}

func (i *capturingSemanticIndex) Search(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error) {
	return nil, errors.New("not implemented")
}

func (i *capturingSemanticIndex) Close() error {
	return nil
}
