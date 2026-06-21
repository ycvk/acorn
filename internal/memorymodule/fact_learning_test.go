package memorymodule

import (
	"fmt"
	"strings"
	"testing"
)

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

func TestCreateFactEquivalentDuplicateIsNoop(t *testing.T) {
	service := newTestService(t)
	first, err := service.CreateFact(t.Context(), CreateFactRequest{
		Title: "VPS IP",
		Body:  "The VPS IP is 1.2.3.4",
		Tags:  []string{"infra"},
	})
	if err != nil {
		t.Fatalf("first CreateFact: %v", err)
	}
	second, err := service.CreateFact(t.Context(), CreateFactRequest{
		Title: "VPS IP",
		Body:  "The VPS IP is 1.2.3.4",
		Tags:  []string{"infra"},
	})
	if err != nil {
		t.Fatalf("duplicate CreateFact must noop, got error: %v", err)
	}
	if second.Ref != first.Ref || second.RelPath != first.RelPath {
		t.Fatalf("duplicate CreateFact returned different record: first=%#v second=%#v", first, second)
	}
	facts, err := service.ListFacts(t.Context(), RecordSelection{IncludeInactive: true})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("len(facts) = %d, want 1 after duplicate noop", len(facts))
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
