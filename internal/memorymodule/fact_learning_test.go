package memorymodule

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// rebuildCountingIndex is a SemanticIndex whose Rebuild succeeds and counts calls,
// so a test can assert that a structured write actually refreshed the semantic
// index (the real fakeSearchSemanticIndex.Rebuild returns "not implemented").
type rebuildCountingIndex struct {
	rebuilds int
}

func (i *rebuildCountingIndex) Rebuild(_ context.Context, _ SemanticRebuildRequest) (*SemanticRebuildResult, error) {
	i.rebuilds++
	return &SemanticRebuildResult{}, nil
}

func (i *rebuildCountingIndex) Search(_ context.Context, _ SemanticSearchRequest) (*SemanticSearchResult, error) {
	return &SemanticSearchResult{}, nil
}

func (i *rebuildCountingIndex) Close() error { return nil }

func TestCreateFactAutoStampsStatusScopeAndTimestamps(t *testing.T) {
	service := newTestService(t)
	rec, err := service.CreateFact(t.Context(), CreateFactRequest{
		Title: "VPS IP",
		Body:  "The VPS IP is 1.2.3.4",
		Tags:  []string{"infra"},
	})
	if err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if rec.Scope != "user" {
		t.Fatalf("default scope = %q, want user", rec.Scope)
	}
	if rec.Status != StatusUnverified {
		t.Fatalf("status = %q, want unverified (backend default)", rec.Status)
	}
	if strings.TrimSpace(rec.Created) == "" || strings.TrimSpace(rec.Updated) == "" {
		t.Fatalf("created/updated must be auto-stamped, got created=%q updated=%q", rec.Created, rec.Updated)
	}
	if !strings.HasPrefix(rec.RelPath, "facts/user/") {
		t.Fatalf("user fact relpath = %q, want under facts/user/", rec.RelPath)
	}
}

func TestCreateFactAllowsEmptyTags(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateFact(t.Context(), CreateFactRequest{Title: "Note", Body: "a loose note"}); err != nil {
		t.Fatalf("CreateFact with no tags must succeed (tags optional): %v", err)
	}
}

func TestCreateFactWorkspaceScopeNestsPath(t *testing.T) {
	service := newTestService(t)
	rec, err := service.CreateFact(t.Context(), CreateFactRequest{
		Title: "Deploy",
		Body:  "deploy steps",
		Scope: "workspace:acorn",
		Tags:  []string{"deploy"},
	})
	if err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if rec.Scope != "workspace:acorn" {
		t.Fatalf("scope = %q", rec.Scope)
	}
	if !strings.HasPrefix(rec.RelPath, "facts/workspaces/acorn/") {
		t.Fatalf("workspace fact relpath = %q, want under facts/workspaces/acorn/", rec.RelPath)
	}
}

// TestCreateFactRebuildsSemanticIndexWhenWired is the regression test for the
// "wrote it but can't recall it" bug: CreateFact must drive the full mutation
// pipeline so a wired semantic runtime is rebuilt, otherwise the just-remembered
// fact is invisible to memory_search/Prepare (which go through the semantic index).
func TestCreateFactRebuildsSemanticIndexWhenWired(t *testing.T) {
	service := newTestService(t)
	index := &rebuildCountingIndex{}
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index:      index,
		Embedder:   deterministicEmbedder{dimensions: 3, model: "text-embedding-3-small"},
		Model:      "text-embedding-3-small",
		Dimensions: 3,
		BatchSize:  64,
		Schema:     SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	if _, err := service.CreateFact(t.Context(), CreateFactRequest{Title: "VPS", Body: "the ip is 1.2.3.4", Tags: []string{"infra"}}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if index.rebuilds == 0 {
		t.Fatal("CreateFact must rebuild the semantic index when a runtime is wired, so the new fact is searchable")
	}
}

// TestCreateFactCanonicalizesWorkspaceScope verifies that a non-canonical workspace
// scope is stored in the canonical form queries use (WorkspaceScope), otherwise the
// fact would be written but never matched by current-workspace recall.
func TestCreateFactCanonicalizesWorkspaceScope(t *testing.T) {
	service := newTestService(t)
	rec, err := service.CreateFact(t.Context(), CreateFactRequest{
		Title: "Deploy",
		Body:  "deploy steps",
		Scope: "workspace:Acorn Prod",
		Tags:  []string{"deploy"},
	})
	if err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if rec.Scope != WorkspaceScope("Acorn Prod") {
		t.Fatalf("stored scope %q must equal the query scope WorkspaceScope(%q)=%q",
			rec.Scope, "Acorn Prod", WorkspaceScope("Acorn Prod"))
	}
}

func TestCreateFactRejectsBlankBody(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateFact(t.Context(), CreateFactRequest{Title: "Empty"}); err == nil {
		t.Fatal("CreateFact with blank body must fail")
	}
}

// TestPlanMemoryMutationIgnoresTimestampsForNoop verifies the dedup fix: a
// re-write of substantively identical content that differs only in the updated
// timestamp is judged noop, not a churny replace (Created/Updated are excluded
// from canonicalComparableRecord now that the backend auto-stamps them).
func TestPlanMemoryMutationIgnoresTimestampsForNoop(t *testing.T) {
	service := newTestService(t)
	base := `---
scope: user
tags:
  - infra
status: unverified
created: 2026-05-01
updated: %s
---

# VPS IP

The VPS IP is 1.2.3.4
`
	writeFile(t, service, "facts/user/vps-ip.md", fmt.Sprintf(base, "2026-05-01"))

	plan, err := service.PlanMemoryMutation(t.Context(), PlanMemoryMutationRequest{
		Path:    "facts/user/vps-ip.md",
		Content: fmt.Sprintf(base, "2026-05-09"), // only `updated` differs
	})
	if err != nil {
		t.Fatalf("PlanMemoryMutation: %v", err)
	}
	if plan.Action != MemoryMutationNoopDuplicate {
		t.Fatalf("timestamp-only change should be noop, got action=%q reason=%q", plan.Action, plan.Reason)
	}
}
