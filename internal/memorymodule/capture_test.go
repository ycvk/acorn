package memorymodule

import (
	"testing"
	"time"
)

func TestSearchCaptureSampleIncludesRefsAndExplainDigest(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go, lint]
status: verified
created: 2026-05-07
updated: 2026-05-07
---

# Acorn Checks

Run make lint.
`)
	setSemanticSearchHits(t, service, []SemanticHit{{
		Ref:   "facts/workspaces/acorn.md#acorn-checks",
		Kind:  KindFact,
		Score: 4,
		Stage: searchStageSemanticHybrid,
	}})
	req := SearchRequest{Query: "go lint", Scope: "workspace:acorn", Limit: 10, Explain: true}
	result, err := service.Search(t.Context(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	sample := SearchCaptureSample(req, result, CaptureMetadata{
		ID:         "sample-search",
		RunID:      "run_1",
		Latency:    25 * time.Millisecond,
		CapturedAt: time.Date(2026, 5, 15, 1, 2, 3, 0, time.UTC),
	})
	if sample.Kind != EvalKindMemorySearch || sample.Query != "go lint" || sample.Scope != "workspace:acorn" {
		t.Fatalf("sample = %#v", sample)
	}
	if sample.RunID != "run_1" || sample.LatencyMS != 25 {
		t.Fatalf("run/latency = %q/%d", sample.RunID, sample.LatencyMS)
	}
	if got, want := sample.ReturnedRefs[0], "facts/workspaces/acorn.md#acorn-checks"; got != want {
		t.Fatalf("returned ref = %q, want %q", got, want)
	}
	if len(sample.ExplainDigest.Stages) == 0 || len(sample.ExplainDigest.Items) == 0 {
		t.Fatalf("missing explain digest: %#v", sample.ExplainDigest)
	}
}

func TestPrepareCaptureSampleUsesRequestRunIDAndResultRefs(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go, lint]
status: verified
created: 2026-05-07
updated: 2026-05-07
---

# Acorn Checks

Run make lint.
`)
	setSemanticSearchHits(t, service, []SemanticHit{{
		Ref:   "facts/workspaces/acorn.md#acorn-checks",
		Kind:  KindFact,
		Score: 4,
		Stage: searchStageSemanticHybrid,
	}})
	req := PrepareRequest{RunID: "run_prepare", WorkspaceSlug: "acorn", UserInput: "go lint", Explain: true}
	result, err := service.Prepare(t.Context(), req)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	sample := PrepareCaptureSample(req, result, CaptureMetadata{Latency: time.Second})
	if sample.Kind != EvalKindMemoryPrepare || sample.RunID != "run_prepare" {
		t.Fatalf("sample = %#v", sample)
	}
	if sample.Scope != "workspace:acorn" || sample.Query != "go lint" || sample.LatencyMS != 1000 {
		t.Fatalf("sample = %#v", sample)
	}
	if len(sample.ReturnedRefs) == 0 {
		t.Fatalf("returned refs empty: %#v", sample)
	}
	if len(sample.ExplainDigest.Items) == 0 {
		t.Fatalf("missing explain digest: %#v", sample.ExplainDigest)
	}
}

func TestSemanticSearchCaptureSampleIncludesSemanticExplainDigest(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/semantic.md", `---
scope: workspace:acorn
tags: [semantic]
status: verified
created: 2026-05-17
updated: 2026-05-17
---

# Semantic Capture

Capture should preserve semantic explain stages.
`)
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index: &fakeSearchSemanticIndex{hits: []SemanticHit{{
			Ref:   "facts/workspaces/semantic.md#semantic-capture",
			Kind:  KindFact,
			Score: 1,
			Stage: searchStageSemanticHybrid,
		}}},
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	req := SearchRequest{Query: "semantic capture query", Scope: "workspace:acorn", Explain: true}
	result, err := service.Search(t.Context(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	sample := SearchCaptureSample(req, result, CaptureMetadata{ID: "semantic-capture"})
	if !stageDigestContains(sample.ExplainDigest.Stages, searchStageSemanticHybrid) {
		t.Fatalf("stages = %#v, want semantic hybrid", sample.ExplainDigest.Stages)
	}
	if len(sample.ExplainDigest.Items) != 1 {
		t.Fatalf("items = %#v, want one digest item", sample.ExplainDigest.Items)
	}
}

func stageDigestContains(stages []EvalStageDigest, name string) bool {
	for _, stage := range stages {
		if stage.Name == name {
			return true
		}
	}
	return false
}
