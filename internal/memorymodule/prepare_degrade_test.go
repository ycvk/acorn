package memorymodule

import (
	"strings"
	"testing"
)

// TestPrepareDegradesWhenSemanticRuntimeUnwired verifies the run-hot-path Prepare
// contract: when no semantic runtime is wired (embedding not configured), Prepare
// degrades to an empty memory result so the run still proceeds, instead of failing
// the whole run. The retained boundary is asserted alongside: an explicit Search on
// the same unwired service still fails loud (no keyword/fake-vector fallback).
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

	// Retained fail-loud boundary: explicit Search still requires a semantic runtime.
	_, searchErr := service.Search(t.Context(), SearchRequest{Query: "please run go lint"})
	if searchErr == nil || !strings.Contains(searchErr.Error(), "semantic search runtime is required") {
		t.Fatalf("explicit Search must still fail loud without a semantic runtime, got: %v", searchErr)
	}
}

// TestPrepareDegradedExplainMarksSemanticUnwired verifies that when a caller asks
// for Explain (eval / replay / debug), the degraded path records a marker stage so
// a "no semantic runtime" sample is distinguishable from a genuine empty-hit result.
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
	if !explainHasStage(result.Explain, searchStageSemanticUnwired) {
		t.Fatalf("degraded Explain should record the semantic_runtime_unwired stage: %#v", result.Explain.Stages)
	}
}
