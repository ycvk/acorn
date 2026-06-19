package memorymodule

import (
	"context"
	"strings"
	"testing"
)

func TestSemanticContractsCompile(t *testing.T) {
	var _ Embedder = fakeEmbedderFunc(func(context.Context, EmbedRequest) (*EmbedResult, error) {
		return &EmbedResult{}, nil
	})
	var _ SemanticIndex = (*fakeSemanticIndex)(nil)
}

func TestValidateEmbedResult(t *testing.T) {
	req := EmbedRequest{Inputs: []EmbedInput{
		{Ref: "facts/a.md#a", Text: "alpha"},
		{Ref: "facts/b.md#b", Text: "beta"},
	}}
	valid := &EmbedResult{
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Vectors: []EmbeddingVector{
			{Ref: "facts/a.md#a", Values: []float32{1, 2, 3}},
			{Ref: "facts/b.md#b", Values: []float32{4, 5, 6}},
		},
	}
	if err := ValidateEmbedResult(req, valid, 3); err != nil {
		t.Fatalf("ValidateEmbedResult valid result error = %v", err)
	}

	tests := []struct {
		name    string
		result  *EmbedResult
		wantErr string
	}{
		{
			name:    "nil",
			result:  nil,
			wantErr: "embed result is required",
		},
		{
			name: "vector count",
			result: &EmbedResult{
				Model:      "text-embedding-3-small",
				Dimensions: 3,
				Vectors:    valid.Vectors[:1],
			},
			wantErr: "embed result vector count = 1, want 2",
		},
		{
			name: "result dimensions",
			result: &EmbedResult{
				Model:      "text-embedding-3-small",
				Dimensions: 2,
				Vectors:    valid.Vectors,
			},
			wantErr: "embed result dimensions = 2, want 3",
		},
		{
			name: "model",
			result: &EmbedResult{
				Model:      "",
				Dimensions: 3,
				Vectors:    valid.Vectors,
			},
			wantErr: "embed result model is required",
		},
		{
			name: "vector ref",
			result: &EmbedResult{
				Model:      "text-embedding-3-small",
				Dimensions: 3,
				Vectors: []EmbeddingVector{
					{Ref: "", Values: []float32{1, 2, 3}},
					{Ref: "facts/b.md#b", Values: []float32{4, 5, 6}},
				},
			},
			wantErr: "embed result vectors[0].ref is required",
		},
		{
			name: "vector dimensions",
			result: &EmbedResult{
				Model:      "text-embedding-3-small",
				Dimensions: 3,
				Vectors: []EmbeddingVector{
					{Ref: "facts/a.md#a", Values: []float32{1, 2}},
					{Ref: "facts/b.md#b", Values: []float32{4, 5, 6}},
				},
			},
			wantErr: `embed result vector "facts/a.md#a" dimensions = 2, want 3`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmbedResult(req, tc.result, 3)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateEmbedResult error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestSemanticRecordFromRecordCopiesRefs(t *testing.T) {
	record := Record{
		Ref:          "facts/a.md#a",
		Kind:         KindFact,
		Scope:        "workspace:acorn",
		Status:       StatusVerified,
		Title:        "A",
		Body:         "Alpha",
		SourceRefs:   []string{"history/h.md#h"},
		EvidenceRefs: []string{"tool_result:1"},
		Updated:      "2026-05-17",
	}
	semantic := SemanticRecordFromRecord(record)
	record.SourceRefs[0] = "changed"
	record.EvidenceRefs[0] = "changed"

	if got, want := semantic.SourceRefs[0], "history/h.md#h"; got != want {
		t.Fatalf("source ref = %q, want %q", got, want)
	}
	if got, want := semantic.EvidenceRefs[0], "tool_result:1"; got != want {
		t.Fatalf("evidence ref = %q, want %q", got, want)
	}
}

func TestSemanticRecordFromRecordCopiesV2Metadata(t *testing.T) {
	record := Record{
		Ref:          "skills/learned/a.md#a",
		Kind:         KindSkill,
		Scope:        "workspace:acorn",
		Status:       StatusVerified,
		Origin:       string(ProcedureOriginActionVerified),
		Title:        "A",
		Body:         "Alpha",
		RelPath:      "skills/learned/a.md",
		TaskPattern:  "memory sync",
		SourceRun:    "run_1",
		SourceRefs:   []string{"facts/a.md#a"},
		EvidenceRefs: []string{"tool_result:1"},
		Relations: []RecordRelation{{
			Type:   RelationSupports,
			Target: "facts/a.md#a",
			Reason: "supports record projection",
		}},
		Created:    "2026-05-18",
		Updated:    "2026-05-18",
		ValidFrom:  "2026-05-17",
		ValidUntil: "2026-12-31",
	}
	semantic := SemanticRecordFromRecord(record)
	record.Relations[0].Target = "changed"
	record.SourceRefs[0] = "changed"

	if got, want := semantic.Origin, string(ProcedureOriginActionVerified); got != want {
		t.Fatalf("origin = %q, want %q", got, want)
	}
	if got, want := semantic.SourceRun, "run_1"; got != want {
		t.Fatalf("source run = %q, want %q", got, want)
	}
	if got, want := semantic.ValidFrom, "2026-05-17"; got != want {
		t.Fatalf("valid_from = %q, want %q", got, want)
	}
	if got, want := semantic.ValidUntil, "2026-12-31"; got != want {
		t.Fatalf("valid_until = %q, want %q", got, want)
	}
	if got, want := semantic.Created, "2026-05-18"; got != want {
		t.Fatalf("created = %q, want %q", got, want)
	}
	if got, want := semantic.Relations[0].Target, "facts/a.md#a"; got != want {
		t.Fatalf("relation target = %q, want %q", got, want)
	}
}

func TestSemanticRecordTextIncludesV2Metadata(t *testing.T) {
	record := SemanticRecord{
		Ref:          "facts/a.md#a",
		Kind:         KindFact,
		Scope:        "workspace:acorn",
		Status:       StatusVerified,
		Origin:       string(ProcedureOriginHuman),
		Title:        "A",
		Body:         "Alpha",
		Path:         "facts/a.md",
		Tags:         []string{"go"},
		TaskPattern:  "memory sync",
		SourceRun:    "run_1",
		SourceRefs:   []string{"facts/b.md#b"},
		EvidenceRefs: []string{"tool_result:1"},
		Relations: []RecordRelation{{
			Type:   RelationDerivedFrom,
			Target: "facts/b.md#b",
			Reason: "source lineage",
		}},
		Created:    "2026-05-18",
		Updated:    "2026-05-18",
		ValidFrom:  "2026-05-17",
		ValidUntil: "2026-12-31",
	}
	text := SemanticRecordText(record)
	for _, want := range []string{
		"origin: human",
		"source_run: run_1",
		"source_refs: facts/b.md#b",
		"evidence_refs: tool_result:1",
		"relations: derived_from facts/b.md#b source lineage",
		"created: 2026-05-18",
		"valid_from: 2026-05-17",
		"valid_until: 2026-12-31",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("semantic text missing %q:\n%s", want, text)
		}
	}
}

func TestSemanticRecordContentHashIsSensitiveToV2Metadata(t *testing.T) {
	record := SemanticRecord{
		Ref:          "facts/a.md#a",
		Kind:         KindFact,
		Scope:        "workspace:acorn",
		Status:       StatusVerified,
		Origin:       string(ProcedureOriginHuman),
		Title:        "A",
		Body:         "body",
		Path:         "facts/a.md",
		Tags:         []string{"go"},
		TaskPattern:  "memory sync",
		SourceRun:    "run_1",
		SourceRefs:   []string{"facts/b.md#b"},
		EvidenceRefs: []string{"tool_result:1"},
		Relations: []RecordRelation{{
			Type:   RelationSupports,
			Target: "facts/b.md#b",
			Reason: "supports",
		}},
		Created:    "2026-05-18",
		Updated:    "2026-05-18",
		ValidFrom:  "2026-05-17",
		ValidUntil: "2026-12-31",
	}
	first, err := SemanticRecordContentHash(record)
	if err != nil {
		t.Fatalf("SemanticRecordContentHash: %v", err)
	}
	record.ValidUntil = "2026-12-30"
	second, err := SemanticRecordContentHash(record)
	if err != nil {
		t.Fatalf("SemanticRecordContentHash second: %v", err)
	}
	if first == second {
		t.Fatal("hash did not change after valid_until changed")
	}
}

func TestSemanticExplainStageConstants(t *testing.T) {
	if searchStageSemanticVector != "semantic_vector" {
		t.Fatalf("searchStageSemanticVector = %q", searchStageSemanticVector)
	}
	if searchStageSemanticFTS != "semantic_fts" {
		t.Fatalf("searchStageSemanticFTS = %q", searchStageSemanticFTS)
	}
	if searchStageSemanticHybrid != "semantic_hybrid" {
		t.Fatalf("searchStageSemanticHybrid = %q", searchStageSemanticHybrid)
	}
}

type fakeEmbedderFunc func(context.Context, EmbedRequest) (*EmbedResult, error)

func (fn fakeEmbedderFunc) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	return fn(ctx, req)
}

type fakeSemanticIndex struct{}

func (*fakeSemanticIndex) Rebuild(context.Context, SemanticRebuildRequest) (*SemanticRebuildResult, error) {
	return &SemanticRebuildResult{}, nil
}

func (*fakeSemanticIndex) Search(context.Context, SemanticSearchRequest) (*SemanticSearchResult, error) {
	return &SemanticSearchResult{}, nil
}

func (*fakeSemanticIndex) Close() error {
	return nil
}
