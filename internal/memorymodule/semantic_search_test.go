package memorymodule

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSearchUsesSemanticRuntimeWhenEnabled(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Acorn Bleve

Semantic retrieval is enabled.
`)
	index := &fakeSearchSemanticIndex{hits: []SemanticHit{{
		Ref:   "facts/workspaces/acorn.md#acorn-bleve",
		Kind:  KindFact,
		Score: 0.9,
		Stage: searchStageSemanticVector,
	}}}
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index:      index,
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Search(t.Context(), SearchRequest{
		Query:   "semantic query that does not contain title words",
		Scope:   "workspace:acorn",
		Explain: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	if got, want := result.Items[0].Ref, "facts/workspaces/acorn.md#acorn-bleve"; got != want {
		t.Fatalf("ref = %q, want %q", got, want)
	}
	if !explainHasStage(result.Explain, searchStageSemanticHybrid) {
		t.Fatalf("explain stages = %#v, want semantic hybrid", result.Explain.Stages)
	}
	if index.called != 1 {
		t.Fatalf("semantic index called %d times", index.called)
	}
	if len(index.last.Vector) != 3 || index.last.Mode != "hybrid" {
		t.Fatalf("semantic search request = %#v", index.last)
	}
}

func TestSearchSemanticFailsWithoutKeywordFallback(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [fallback]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Fallback Match

fallback keyword exists.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index:      &fakeSearchSemanticIndex{},
		Embedder:   failingEmbedder{err: errors.New("embedding down")},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	_, err := service.Search(t.Context(), SearchRequest{Query: "fallback"})
	if err == nil || !strings.Contains(err.Error(), "embedding down") {
		t.Fatalf("Search error = %v", err)
	}
}

func TestSearchRequiresSemanticRuntime(t *testing.T) {
	service := newTestService(t)
	_, err := service.Search(t.Context(), SearchRequest{Query: "keyword-only"})
	if err == nil || !strings.Contains(err.Error(), "semantic search runtime is required") {
		t.Fatalf("Search error = %v, want semantic runtime required error", err)
	}
}

func TestSearchSemanticFailsOnMissingCanonicalRef(t *testing.T) {
	service := newTestService(t)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/missing.md#missing",
			Kind:  KindFact,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	_, err := service.Search(t.Context(), SearchRequest{Query: "missing"})
	if err == nil || !strings.Contains(err.Error(), "resolve semantic hit") {
		t.Fatalf("Search error = %v", err)
	}
}

func TestSearchSemanticPreservesSourceRefBacklink(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/source.md", `---
scope: workspace:acorn
tags: [source]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Source Record

Canonical source.
`)
	writeFile(t, service, "skills/learned/procedure.md", `---
origin: action_verified
task_pattern: semantic backlink
status: verified
created: 2026-05-17
updated: 2026-05-17
source_run: run-1
source_refs:
  - facts/workspaces/source.md#source-record
evidence_refs:
  - tool_result:1
---

# Procedure Record

Procedure cites source.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "skills/learned/procedure.md#procedure-record",
			Kind:  KindSkill,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Search(t.Context(), SearchRequest{Query: "backlink", Scope: "workspace:acorn", Explain: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !containsSearchRef(result.Items, "facts/workspaces/source.md#source-record") {
		t.Fatalf("items missing backlink source: %#v", result.Items)
	}
	if !explainHasStage(result.Explain, searchStageSourceRefBacklink) {
		t.Fatalf("explain stages = %#v, want source ref backlink", result.Explain.Stages)
	}
}

func TestSearchSemanticAppliesActiveSelectionFlags(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/active.md", `---
scope: workspace:acorn
tags: [semantic]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Active Semantic

Active hit.
`)
	writeFile(t, service, "facts/workspaces/expired.md", `---
scope: workspace:acorn
tags: [semantic]
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
---

# Expired Semantic

Expired hit.
`)
	writeFile(t, service, "facts/workspaces/retired.md", `---
scope: workspace:acorn
tags: [semantic]
status: retired
created: 2026-05-17
updated: 2026-05-17
---

# Retired Semantic

Retired hit.
`)
	index := &fakeSearchSemanticIndex{hits: []SemanticHit{
		{Ref: "facts/workspaces/active.md#active-semantic", Kind: KindFact, Score: 3, Stage: searchStageSemanticVector},
		{Ref: "facts/workspaces/expired.md#expired-semantic", Kind: KindFact, Score: 2, Stage: searchStageSemanticVector},
		{Ref: "facts/workspaces/retired.md#retired-semantic", Kind: KindFact, Score: 1, Stage: searchStageSemanticVector},
	}}
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index:      index,
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	active, err := service.Search(t.Context(), SearchRequest{Query: "semantic", Scope: "workspace:acorn"})
	if err != nil {
		t.Fatalf("Search active: %v", err)
	}
	if containsSearchRef(active.Items, "facts/workspaces/expired.md#expired-semantic") || containsSearchRef(active.Items, "facts/workspaces/retired.md#retired-semantic") {
		t.Fatalf("active search returned inactive records: %#v", active.Items)
	}
	inactive, err := service.Search(t.Context(), SearchRequest{Query: "semantic", Scope: "workspace:acorn", IncludeInactive: true})
	if err != nil {
		t.Fatalf("Search inactive: %v", err)
	}
	if !containsSearchRef(inactive.Items, "facts/workspaces/expired.md#expired-semantic") {
		t.Fatalf("include inactive search missing expired record: %#v", inactive.Items)
	}
	if containsSearchRef(inactive.Items, "facts/workspaces/retired.md#retired-semantic") {
		t.Fatalf("include inactive search returned retired record: %#v", inactive.Items)
	}
	retired, err := service.Search(t.Context(), SearchRequest{Query: "semantic", Scope: "workspace:acorn", IncludeRetired: true})
	if err != nil {
		t.Fatalf("Search retired: %v", err)
	}
	if !containsSearchRef(retired.Items, "facts/workspaces/retired.md#retired-semantic") {
		t.Fatalf("include retired search missing retired record: %#v", retired.Items)
	}
	if !index.last.IncludeRetired {
		t.Fatalf("semantic index request did not receive IncludeRetired")
	}
}

func TestSearchSemanticSourceRefBoostSkipsInactiveTargets(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/expired-source.md", `---
scope: workspace:acorn
tags: [source]
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
---

# Expired Source

Expired source fact.
`)
	writeFile(t, service, "skills/learned/procedure.md", `---
origin: action_verified
task_pattern: inactive source boost
status: verified
created: 2026-05-17
updated: 2026-05-17
source_run: run-1
source_refs:
  - facts/workspaces/expired-source.md#expired-source
evidence_refs:
  - tool_result:1
---

# Procedure With Expired Source

Procedure cites expired source.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "skills/learned/procedure.md#procedure-with-expired-source",
			Kind:  KindSkill,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Search(t.Context(), SearchRequest{Query: "inactive source", Scope: "workspace:acorn"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if containsSearchRef(result.Items, "facts/workspaces/expired-source.md#expired-source") {
		t.Fatalf("source-ref boost reintroduced inactive target: %#v", result.Items)
	}
	inactive, err := service.Search(t.Context(), SearchRequest{Query: "inactive source", Scope: "workspace:acorn", IncludeInactive: true})
	if err != nil {
		t.Fatalf("Search include inactive: %v", err)
	}
	if !containsSearchRef(inactive.Items, "facts/workspaces/expired-source.md#expired-source") {
		t.Fatalf("include inactive search missing inactive source-ref boost target: %#v", inactive.Items)
	}
}

func TestSearchSemanticAppliesRelationBoost(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/target.md", `---
scope: workspace:acorn
tags: [relation]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Supported Target

Relation target.
`)
	writeFile(t, service, "facts/workspaces/matched.md", `---
scope: workspace:acorn
tags: [relation]
status: verified
created: 2026-05-17
updated: 2026-05-17
relations:
  - type: supports
    target: facts/workspaces/target.md#supported-target
    reason: matched fact supports target
---

# Matched Source

Matched source.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/matched.md#matched-source",
			Kind:  KindFact,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Search(t.Context(), SearchRequest{Query: "relation", Scope: "workspace:acorn", Explain: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !containsSearchRef(result.Items, "facts/workspaces/target.md#supported-target") {
		t.Fatalf("relation boost missing target: %#v", result.Items)
	}
	if !explainHasStage(result.Explain, searchStageRelationSupports) {
		t.Fatalf("explain stages = %#v, want relation supports", result.Explain.Stages)
	}
	if !explainHasContributionSource(result.Explain, "facts/workspaces/target.md#supported-target", "facts/workspaces/matched.md#matched-source") {
		t.Fatalf("explain missing matched relation source: %#v", result.Explain)
	}
}

func TestSearchSemanticRelationBoostRespectsInactiveSelection(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/expired-target.md", `---
scope: workspace:acorn
tags: [relation]
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
---

# Expired Relation Target

Expired target.
`)
	writeFile(t, service, "facts/workspaces/matched.md", `---
scope: workspace:acorn
tags: [relation]
status: verified
created: 2026-05-17
updated: 2026-05-17
relations:
  - type: derived_from
    target: facts/workspaces/expired-target.md#expired-relation-target
---

# Matched Relation Source

Matched source.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/matched.md#matched-relation-source",
			Kind:  KindFact,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	active, err := service.Search(t.Context(), SearchRequest{Query: "relation", Scope: "workspace:acorn"})
	if err != nil {
		t.Fatalf("Search active: %v", err)
	}
	if containsSearchRef(active.Items, "facts/workspaces/expired-target.md#expired-relation-target") {
		t.Fatalf("active search returned inactive relation target: %#v", active.Items)
	}
	inactive, err := service.Search(t.Context(), SearchRequest{Query: "relation", Scope: "workspace:acorn", IncludeInactive: true})
	if err != nil {
		t.Fatalf("Search inactive: %v", err)
	}
	if !containsSearchRef(inactive.Items, "facts/workspaces/expired-target.md#expired-relation-target") {
		t.Fatalf("include inactive search missing relation target: %#v", inactive.Items)
	}
}

func TestSearchSemanticSurfacesContradictionRelation(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/conflict.md", `---
scope: workspace:acorn
tags: [relation]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Conflict Target

Conflicting context.
`)
	writeFile(t, service, "facts/workspaces/matched.md", `---
scope: workspace:acorn
tags: [relation]
status: verified
created: 2026-05-17
updated: 2026-05-17
relations:
  - type: contradicts
    target: facts/workspaces/conflict.md#conflict-target
---

# Matched Contradiction

Matched context.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/matched.md#matched-contradiction",
			Kind:  KindFact,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Search(t.Context(), SearchRequest{Query: "contradiction", Scope: "workspace:acorn", Explain: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !containsSearchRef(result.Items, "facts/workspaces/conflict.md#conflict-target") {
		t.Fatalf("contradiction target not surfaced: %#v", result.Items)
	}
	if !explainHasStage(result.Explain, searchStageRelationContradict) {
		t.Fatalf("explain stages = %#v, want relation contradict", result.Explain.Stages)
	}
}

func TestSearchSemanticFailsOnMissingSourceRef(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "skills/learned/procedure.md", `---
origin: action_verified
task_pattern: semantic missing source
status: verified
created: 2026-05-17
updated: 2026-05-17
source_run: run-1
source_refs:
  - facts/workspaces/missing.md#missing-source
evidence_refs:
  - tool_result:1
---

# Missing Source Procedure

Procedure cites a missing canonical source.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "skills/learned/procedure.md#missing-source-procedure",
			Kind:  KindSkill,
			Score: 1,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	_, err := service.Search(t.Context(), SearchRequest{Query: "missing source", Scope: "workspace:acorn"})
	if err == nil || !strings.Contains(err.Error(), "resolve source-ref boost") {
		t.Fatalf("Search error = %v, want missing source-ref failure", err)
	}
}

func TestPrepareInheritsSemanticSearch(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/semantic.md", `---
scope: workspace:acorn
tags: [semantic]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Semantic Entry

Injected through semantic search.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/semantic.md#semantic-entry",
			Kind:  KindFact,
			Score: 4,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "question without lexical match",
		MaxNudges:     1,
		MaxEntries:    1,
		Explain:       true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(result.Nudges) != 1 || result.Nudges[0].Ref != "facts/workspaces/semantic.md#semantic-entry" {
		t.Fatalf("nudges = %#v", result.Nudges)
	}
	if len(result.Entries) != 1 || result.Entries[0].Ref != "facts/workspaces/semantic.md#semantic-entry" {
		t.Fatalf("entries = %#v", result.Entries)
	}
	if !explainHasStage(result.Explain, searchStageSemanticHybrid) {
		t.Fatalf("explain stages = %#v, want semantic hybrid", result.Explain.Stages)
	}
}

func TestPrepareSkipsInactiveSemanticHits(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/expired.md", `---
scope: workspace:acorn
tags: [semantic]
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
---

# Expired Entry

Expired semantic memory.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/expired.md#expired-entry",
			Kind:  KindFact,
			Score: 4,
			Stage: searchStageSemanticVector,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "vector",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "expired semantic memory",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(result.Nudges) != 0 || len(result.Entries) != 0 {
		t.Fatalf("prepare returned inactive records: nudges=%#v entries=%#v", result.Nudges, result.Entries)
	}
}

func TestSemanticRetrievalReplayBaselineFixtures(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/semantic.md", `---
scope: workspace:acorn
tags: [semantic, bleve]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Semantic Bleve Fact

Semantic retrieval should find this without lexical overlap.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/semantic.md#semantic-bleve-fact",
			Kind:  KindFact,
			Score: 3,
			Stage: searchStageSemanticHybrid,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}

	searchResult, err := service.Search(t.Context(), SearchRequest{
		Query:   "question with no lexical overlap",
		Scope:   "workspace:acorn",
		Limit:   10,
		Explain: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertSearchRefs(t, searchResult.Items, []string{"facts/workspaces/semantic.md#semantic-bleve-fact"})
	if !explainHasStage(searchResult.Explain, searchStageSemanticHybrid) {
		t.Fatalf("search explain stages = %#v, want semantic hybrid", searchResult.Explain.Stages)
	}

	prepareResult, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "another non lexical prompt",
		MaxNudges:     1,
		MaxEntries:    1,
		Explain:       true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	assertEntryRefs(t, prepareResult.Entries, []string{"facts/workspaces/semantic.md#semantic-bleve-fact"})
	if !explainHasStage(prepareResult.Explain, searchStageSemanticHybrid) {
		t.Fatalf("prepare explain stages = %#v, want semantic hybrid", prepareResult.Explain.Stages)
	}
}

type fakeSearchSemanticIndex struct {
	called int
	last   SemanticSearchRequest
	hits   []SemanticHit
	err    error
}

func (i *fakeSearchSemanticIndex) Rebuild(ctx context.Context, req SemanticRebuildRequest) (*SemanticRebuildResult, error) {
	return nil, errors.New("not implemented")
}

func (i *fakeSearchSemanticIndex) Search(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error) {
	i.called++
	i.last = req
	if i.err != nil {
		return nil, i.err
	}
	return &SemanticSearchResult{Hits: append([]SemanticHit(nil), i.hits...)}, nil
}

func (i *fakeSearchSemanticIndex) Close() error {
	return nil
}
