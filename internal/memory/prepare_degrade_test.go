package memory

import (
	"testing"
)

// TestPrepareDegradesWhenSemanticRuntimeUnwired verifies the run-hot-path Prepare
// contract: when no semantic runtime is wired (embedding not configured), Prepare
// degrades to an empty memory result so the run still proceeds, instead of failing
// the whole run. The retained boundary is asserted alongside: an explicit Search on
// the same unwired service falls back to keyword matching (no fake-vector path),
// returning an empty result when no records match rather than failing the run.
func TestPrepareDegradesWhenSemanticRuntimeUnwired(t *testing.T) {
	service := newTestService(t)

	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "please run go lint",
	})
	if err != nil {
		t.Fatalf("Prepare on unwired semantic runtime should degrade, got error: %v", err)
	}
	if result == nil {
		t.Fatal("Prepare returned nil result")
	}
	if len(result.Nudges) != 0 || len(result.Entries) != 0 {
		t.Fatalf("degraded Prepare should return no nudges/entries, got nudges=%d entries=%d",
			len(result.Nudges), len(result.Entries))
	}

	// Retained boundary: explicit Search falls back to keyword matching on an
	// unwired service, returning an empty result (no matching records) instead of
	// failing the run.
	searchResult, searchErr := service.Search(t.Context(), SearchRequest{Query: "please run go lint"})
	if searchErr != nil {
		t.Fatalf("explicit Search should fall back to keyword matching, not fail: %v", searchErr)
	}
	if searchResult == nil {
		t.Fatal("Search returned nil result")
	}
}

// TestPrepareDegradedExplainMarksSemanticUnwired verifies that when a caller asks
// for Explain (eval / replay / debug), the degraded path records the keyword-match
// stage so a keyword-only sample is distinguishable from a semantic-vector search.
func TestPrepareDegradedExplainMarksSemanticUnwired(t *testing.T) {
	service := newTestService(t)

	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "please run go lint",
		Explain:       true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result.Explain == nil {
		t.Fatal("degraded Prepare with Explain=true should carry an Explain marker")
	}
	if !explainHasStage(result.Explain, searchStageKeyword) {
		t.Fatalf("degraded Explain should record the keyword_match stage: %#v", result.Explain.Stages)
	}
}
