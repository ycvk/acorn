package memorymodule

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureLayoutCreatesMemoryDirectories(t *testing.T) {
	service := newTestService(t)
	if err := service.EnsureLayout(t.Context()); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	for _, rel := range []string{
		"facts/user",
		"facts/workspaces",
		"skills/built-in",
		"skills/learned",
		"history",
	} {
		info, err := os.Stat(filepath.Join(service.Root(), rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", rel)
		}
	}
}

func TestListFactsParsesFrontmatter(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "history/session.md", "- 2026-05-07T00:00:00Z run_123 succeeded source history.\n")
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go, lint, format]
status: verified
created: 2026-05-07
updated: 2026-05-07
valid_from: 2026-05-07
valid_until: 2026-12-31
source_run: run_123
source_refs:
  - history/session.md
evidence_refs:
  - tool_result:run_123:call_1
relations:
  - type: derived_from
    target: history/session.md
    reason: extracted from verified run evidence
---

# Acorn Development Checks

- Run make lint.
`)
	facts, err := service.ListFacts(t.Context(), RecordSelection{})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("len(facts) = %d, want 1", len(facts))
	}
	fact := facts[0]
	if fact.Ref != "facts/workspaces/acorn.md#acorn-development-checks" {
		t.Fatalf("ref = %q", fact.Ref)
	}
	if fact.Scope != "workspace:acorn" || fact.Status != StatusVerified || fact.Title != "Acorn Development Checks" {
		t.Fatalf("unexpected fact: %#v", fact)
	}
	if strings.Join(fact.Tags, ",") != "format,go,lint" {
		t.Fatalf("tags = %#v", fact.Tags)
	}
	if fact.ValidFrom != "2026-05-07" || fact.ValidUntil != "2026-12-31" {
		t.Fatalf("validity = %q/%q", fact.ValidFrom, fact.ValidUntil)
	}
	if got, want := strings.Join(fact.SourceRefs, ","), "history/session.md"; got != want {
		t.Fatalf("source refs = %q, want %q", got, want)
	}
	if got, want := strings.Join(fact.EvidenceRefs, ","), "tool_result:run_123:call_1"; got != want {
		t.Fatalf("evidence refs = %q, want %q", got, want)
	}
	if len(fact.Relations) != 1 || fact.Relations[0].Type != RelationDerivedFrom || fact.Relations[0].Target != "history/session.md" {
		t.Fatalf("relations = %#v", fact.Relations)
	}
}

func TestListFactsRejectsInvalidFrontmatter(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "facts", "workspaces", "bad.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
scope: project:acorn
tags: [go]
status: verified
created: 2026-05-07
updated: 2026-05-07
---

# Bad
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "scope must be user or workspace") {
		t.Fatalf("BuildIndex error = %v, want scope validation", err)
	}
}

func TestListFactsRejectsUnknownFrontmatterField(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "facts", "workspaces", "old-source.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-07
updated: 2026-05-07
source: old free text source
---

# Old Source
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "field source not found") {
		t.Fatalf("BuildIndex error = %v, want unknown field rejection", err)
	}
}

func TestListFactsRejectsInvalidRelation(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "facts", "workspaces", "bad-relation.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-07
updated: 2026-05-07
relations:
  - type: maybe
    target: facts/a.md#a
---

# Bad Relation
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "relation type must be") {
		t.Fatalf("BuildIndex error = %v, want relation type rejection", err)
	}
}

func TestListFactsReturnsActiveRecordsOnly(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/old.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-07
updated: 2026-05-07
---

# Old Fact

Old active fact.
`)
	writeFile(t, service, "facts/workspaces/new.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-07
updated: 2026-05-07
relations:
  - type: supersedes
    target: facts/workspaces/old.md#old-fact
---

# New Fact

Replacement fact.
`)
	writeFile(t, service, "facts/workspaces/expired.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
---

# Expired Fact

Expired fact.
`)
	writeFile(t, service, "facts/workspaces/future.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-07
updated: 2026-05-07
valid_from: 2999-01-01
---

# Future Fact

Future fact.
`)
	writeFile(t, service, "facts/workspaces/retired.md", `---
scope: workspace:acorn
tags: [memory]
status: retired
created: 2026-05-07
updated: 2026-05-07
---

# Retired Fact

Retired fact.
`)
	facts, err := service.ListFacts(t.Context(), RecordSelection{})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("len(facts) = %d, want 1: %#v", len(facts), facts)
	}
	if got, want := facts[0].Ref, "facts/workspaces/new.md#new-fact"; got != want {
		t.Fatalf("active ref = %q, want %q", got, want)
	}
}

func TestSelectRecordsIncludeInactiveAndRetired(t *testing.T) {
	records := []Record{
		{Ref: "active", Status: StatusVerified, Created: "2026-05-07", Updated: "2026-05-07"},
		{Ref: "expired", Status: StatusVerified, Created: "2020-01-01", Updated: "2020-01-01", ValidUntil: "2020-01-02"},
		{Ref: "retired", Status: StatusRetired, Created: "2026-05-07", Updated: "2026-05-07"},
	}
	inactive, err := SelectRecords(records, RecordSelection{IncludeInactive: true, Now: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("SelectRecords inactive: %v", err)
	}
	if len(inactive) != 2 {
		t.Fatalf("inactive len = %d, want 2: %#v", len(inactive), inactive)
	}
	all, err := SelectRecords(records, RecordSelection{IncludeRetired: true, Now: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("SelectRecords retired: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("retired len = %d, want 3: %#v", len(all), all)
	}
}

func TestBuildIndexFailsOnMissingSupersedesTarget(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "facts", "workspaces", "bad-supersedes.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-07
updated: 2026-05-07
relations:
  - type: supersedes
    target: facts/workspaces/missing.md#missing
---

# Bad Supersedes
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "supersedes relation target") {
		t.Fatalf("BuildIndex error = %v, want missing supersedes target", err)
	}
}

func TestBuildIndexFailsOnMissingRelationTarget(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "facts", "workspaces", "bad-supports.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-07
updated: 2026-05-07
relations:
  - type: supports
    target: facts/workspaces/missing.md#missing
---

# Bad Supports
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "supports relation target") {
		t.Fatalf("BuildIndex error = %v, want missing relation target", err)
	}
}

func TestListSkillsParsesProcedureFrontmatter(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go]
status: verified
created: 2026-05-08
updated: 2026-05-08
---

# Acorn Development Checks

Run tests.
`)
	writeFile(t, service, "skills/learned/review.md", `---
origin: action_verified
task_pattern: review, test, verify
status: verified
created: 2026-05-08
updated: 2026-05-08
valid_from: 2026-05-08
source_run: run_verify
source_refs:
  - history/session.md#run-verify
evidence_refs:
  - tool-result:run_verify:call_test
relations:
  - type: supports
    target: facts/workspaces/acorn.md#acorn-development-checks
    reason: uses project verification checks
---

# Review Procedure

Run tests and inspect results.
`)
	skills, err := service.ListSkills(t.Context(), RecordSelection{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(skills))
	}
	procedure, err := ProcedureRecordFromMemoryRecord(skills[0])
	if err != nil {
		t.Fatalf("ProcedureRecordFromMemoryRecord: %v", err)
	}
	if procedure.Origin != ProcedureOriginActionVerified || procedure.Status != StatusVerified {
		t.Fatalf("procedure = %#v, want action_verified verified", procedure)
	}
	if got, want := strings.Join(procedure.EvidenceRefs, ","), "tool-result:run_verify:call_test"; got != want {
		t.Fatalf("evidence refs = %q, want %q", got, want)
	}
	if skills[0].ValidFrom != "2026-05-08" {
		t.Fatalf("valid_from = %q", skills[0].ValidFrom)
	}
	if len(skills[0].Relations) != 1 || skills[0].Relations[0].Type != RelationSupports {
		t.Fatalf("relations = %#v", skills[0].Relations)
	}
}

func TestCreateProcedureWritesActionVerifiedSkill(t *testing.T) {
	service := newTestService(t)
	procedure, err := service.CreateProcedure(t.Context(), CreateProcedureRequest{
		Title:        "SQLite Rows Close Procedure",
		TaskPattern:  "sqlite, rows, close",
		Body:         "Always close rows and check rows.Err after iteration.",
		SourceRun:    "run_verify",
		SourceRefs:   []string{"history/session.md#run-verify"},
		EvidenceRefs: []string{"tool_result:run_verify:call_test"},
	})
	if err != nil {
		t.Fatalf("CreateProcedure: %v", err)
	}
	if procedure.Ref != "skills/learned/sqlite-rows-close-procedure.md#sqlite-rows-close-procedure" {
		t.Fatalf("Ref = %q", procedure.Ref)
	}
	if procedure.Origin != ProcedureOriginActionVerified || procedure.Status != StatusVerified {
		t.Fatalf("procedure = %#v, want action_verified verified", procedure)
	}
	if procedure.SourceRun != "run_verify" || len(procedure.EvidenceRefs) != 1 {
		t.Fatalf("procedure source/evidence = %#v", procedure)
	}
	if procedure.MutationPlan == nil || procedure.MutationPlan.Action != MemoryMutationCreate {
		t.Fatalf("mutation plan = %#v, want create", procedure.MutationPlan)
	}
	path := filepath.Join(service.Root(), "skills", "learned", "sqlite-rows-close-procedure.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"source_refs:",
		"evidence_refs:",
		"origin: action_verified",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("procedure file missing %q:\n%s", want, content)
		}
	}
}

func TestCreateProcedureRequiresEvidenceRefs(t *testing.T) {
	service := newTestService(t)
	_, err := service.CreateProcedure(t.Context(), CreateProcedureRequest{
		Title:       "Missing Evidence",
		TaskPattern: "verify",
		Body:        "Do verified work.",
		SourceRun:   "run_verify",
	})
	if err == nil || !strings.Contains(err.Error(), "action_verified procedure evidence_refs are required") {
		t.Fatalf("error = %v, want evidence refs error", err)
	}
}

func TestPlanMemoryMutationCreateFactDoesNotWrite(t *testing.T) {
	service := newTestService(t)
	plan, err := service.PlanMemoryMutation(t.Context(), PlanMemoryMutationRequest{
		Path:    "facts/workspaces/new.md",
		Content: mutationPlanFactContent("verified", "New Fact", "New fact body."),
	})
	if err != nil {
		t.Fatalf("PlanMemoryMutation: %v", err)
	}
	if plan.Action != MemoryMutationCreate || plan.Ref != "facts/workspaces/new.md#new-fact" {
		t.Fatalf("plan = %#v, want create for new fact", plan)
	}
	if _, err := os.Stat(filepath.Join(service.Root(), "facts", "workspaces", "new.md")); !os.IsNotExist(err) {
		t.Fatalf("planner wrote file or stat failed: %v", err)
	}
}

func TestPlanMemoryMutationReplaceRetireAndNoop(t *testing.T) {
	service := newTestService(t)
	original := mutationPlanFactContent("verified", "Existing Fact", "Original body.")
	writeFile(t, service, "facts/workspaces/existing.md", original)

	replace, err := service.PlanMemoryMutation(t.Context(), PlanMemoryMutationRequest{
		Path:    "facts/workspaces/existing.md",
		Content: mutationPlanFactContent("verified", "Existing Fact", "Changed body."),
	})
	if err != nil {
		t.Fatalf("PlanMemoryMutation replace: %v", err)
	}
	if replace.Action != MemoryMutationReplaceExisting {
		t.Fatalf("replace action = %q, want replace_existing: %#v", replace.Action, replace)
	}

	retire, err := service.PlanMemoryMutation(t.Context(), PlanMemoryMutationRequest{
		Path:    "facts/workspaces/existing.md",
		Content: mutationPlanFactContent("retired", "Existing Fact", "Original body."),
	})
	if err != nil {
		t.Fatalf("PlanMemoryMutation retire: %v", err)
	}
	if retire.Action != MemoryMutationRetireExisting {
		t.Fatalf("retire action = %q, want retire_existing: %#v", retire.Action, retire)
	}

	noop, err := service.PlanMemoryMutation(t.Context(), PlanMemoryMutationRequest{
		Path:    "facts/workspaces/existing.md",
		Content: original,
	})
	if err != nil {
		t.Fatalf("PlanMemoryMutation noop: %v", err)
	}
	if noop.Action != MemoryMutationNoopDuplicate {
		t.Fatalf("noop action = %q, want noop_duplicate: %#v", noop.Action, noop)
	}
}

func TestPlanMemoryMutationRejectsInvalidPathSchemaAndRelation(t *testing.T) {
	service := newTestService(t)
	for _, tc := range []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{
			name:    "history path",
			path:    "history/session.md",
			content: "not a memory record",
			want:    "under facts/ or skills",
		},
		{
			name: "unknown field",
			path: "facts/workspaces/bad.md",
			content: `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
source: old field
---

# Bad Fact

Bad.
`,
			want: "field source not found",
		},
		{
			name: "missing relation target",
			path: "facts/workspaces/relation.md",
			content: `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
relations:
  - type: supports
    target: facts/workspaces/missing.md#missing
---

# Relation Fact

Bad relation.
`,
			want: "supports relation target",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := service.PlanMemoryMutation(t.Context(), PlanMemoryMutationRequest{
				Path:    tc.path,
				Content: tc.content,
			})
			if err != nil {
				t.Fatalf("PlanMemoryMutation: %v", err)
			}
			if plan.Action != MemoryMutationRejectInvalid || !strings.Contains(plan.Reason, tc.want) {
				t.Fatalf("plan = %#v, want reject containing %q", plan, tc.want)
			}
		})
	}
}

func mutationPlanFactContent(status string, title string, body string) string {
	return `---
scope: workspace:acorn
tags: [memory]
status: ` + status + `
created: 2026-05-18
updated: 2026-05-18
---

# ` + title + `

` + body + `
`
}

func TestListSkillsRejectsUnknownFrontmatterField(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "skills", "learned", "bad.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
origin: action_verified
task_pattern: verify
status: verified
created: 2026-05-08
updated: 2026-05-08
source_run: run_verify
evidence_refs:
  - tool_result:run_verify:call_test
legacy_field: nope
---

# Bad Skill
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "field legacy_field not found") {
		t.Fatalf("BuildIndex error = %v, want unknown field rejection", err)
	}
}

func TestListSkillsRejectsOldProcedureOrigin(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "skills", "learned", "deploy.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
origin: learned
task_pattern: deploy
status: verified
created: 2026-05-08
updated: 2026-05-08
---

# Deploy
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "procedure origin must be human, agent_draft, or action_verified") {
		t.Fatalf("BuildIndex error = %v, want old origin rejection", err)
	}
}

func TestListSkillsRejectsAgentDraftWithoutSourceRun(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "skills", "learned", "draft.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
origin: agent_draft
task_pattern: draft
status: unverified
created: 2026-05-08
updated: 2026-05-08
---

# Draft
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "agent_draft procedure source_run is required") {
		t.Fatalf("BuildIndex error = %v, want source_run rejection", err)
	}
}

func TestListSkillsRejectsActionVerifiedWithoutEvidenceRefs(t *testing.T) {
	service := newTestService(t)
	path := filepath.Join(service.Root(), "skills", "learned", "verified.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`---
origin: action_verified
task_pattern: verify
status: verified
created: 2026-05-08
updated: 2026-05-08
source_run: run_verify
---

# Verified
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := service.BuildIndex(t.Context())
	if err == nil || !strings.Contains(err.Error(), "action_verified procedure evidence_refs are required") {
		t.Fatalf("BuildIndex error = %v, want evidence refs error", err)
	}
}

func TestSkillTreeContainsActiveSkillsOnly(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "skills/learned/active.md", `---
origin: action_verified
task_pattern: active, procedure
status: verified
created: 2026-05-08
updated: 2026-05-08
source_run: run_active
evidence_refs:
  - tool_result:run_active:call_test
---

# Active Procedure

Active skill.
`)
	writeFile(t, service, "skills/learned/expired.md", `---
origin: action_verified
task_pattern: expired, procedure
status: verified
created: 2020-01-01
updated: 2020-01-01
valid_until: 2020-01-02
source_run: run_expired
evidence_refs:
  - tool_result:run_expired:call_test
---

# Expired Procedure

Expired skill.
`)
	tree := service.GetSkillTree()
	if tree == nil {
		t.Fatalf("skill tree is nil")
	}
	if _, ok := tree.Categories["active"].Skills["active procedure"]; !ok {
		t.Fatalf("active skill missing from tree: %#v", tree.Categories)
	}
	if category, ok := tree.Categories["expired"]; ok {
		if _, exists := category.Skills["expired procedure"]; exists {
			t.Fatalf("expired skill should not be in tree: %#v", tree.Categories)
		}
	}
}

func TestSearchRejectsLimitAboveExplicitMaximum(t *testing.T) {
	service := newTestService(t)
	setSemanticSearchHits(t, service, nil)
	_, err := service.Search(t.Context(), SearchRequest{Query: "anything", Limit: maxSearchLimit + 1})
	if err == nil || !strings.Contains(err.Error(), "search limit") {
		t.Fatalf("Search error = %v, want explicit limit error", err)
	}
}

func TestPrepareReturnsNudgesAndVerifiedEntries(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "facts/workspaces/acorn.md", `---
scope: workspace:acorn
tags: [go, lint, format]
status: verified
created: 2026-05-07
updated: 2026-05-07
---

# Acorn Development Checks

- Run make lint.
`)
	writeFile(t, service, "facts/workspaces/retired.md", `---
scope: workspace:acorn
tags: [lint]
status: retired
created: 2026-05-07
updated: 2026-05-07
---

# Retired Check

Old instruction.
`)
	setSemanticSearchHits(t, service, []SemanticHit{{
		Ref:   "facts/workspaces/acorn.md#acorn-development-checks",
		Kind:  KindFact,
		Score: 4,
		Stage: searchStageSemanticHybrid,
	}})
	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "please run go lint",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(result.Nudges) != 1 {
		t.Fatalf("len(nudges) = %d, want 1: %#v", len(result.Nudges), result.Nudges)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1: %#v", len(result.Entries), result.Entries)
	}
	if !strings.Contains(result.Entries[0].Content, "Run make lint") {
		t.Fatalf("entry content = %q", result.Entries[0].Content)
	}
}

func TestPrepareDoesNotInjectAgentDraftProcedure(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "skills/learned/sqlite.md", `---
origin: agent_draft
task_pattern: sqlite, query, rows
status: unverified
created: 2026-05-08
updated: 2026-05-08
source_run: run_draft
---

# SQLite Query Loop Procedure

Always close rows and check rows.Err.
`)
	setSemanticSearchHits(t, service, []SemanticHit{{
		Ref:   "skills/learned/sqlite.md#sqlite-query-loop-procedure",
		Kind:  KindSkill,
		Score: 4,
		Stage: searchStageSemanticHybrid,
	}})
	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "fix sqlite query rows",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(result.Nudges) != 1 {
		t.Fatalf("len(nudges) = %d, want 1: %#v", len(result.Nudges), result.Nudges)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0: %#v", len(result.Entries), result.Entries)
	}
	if !hasProcedureActivation(result.ProcedureActivations, ProcedureActivationMatched, "skills/learned/sqlite.md#sqlite-query-loop-procedure") {
		t.Fatalf("missing matched activation: %#v", result.ProcedureActivations)
	}
	if result.SkillTree == nil {
		t.Fatalf("expected skill tree in result")
	}
}

func TestPrepareInjectsActionVerifiedProcedure(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "skills/learned/sqlite.md", `---
origin: action_verified
task_pattern: sqlite, query, rows
status: verified
created: 2026-05-08
updated: 2026-05-08
source_run: run_verified
evidence_refs:
  - tool-result:run_verified:call_test
---

# SQLite Query Loop Procedure

Always close rows and check rows.Err.
`)
	setSemanticSearchHits(t, service, []SemanticHit{{
		Ref:   "skills/learned/sqlite.md#sqlite-query-loop-procedure",
		Kind:  KindSkill,
		Score: 4,
		Stage: searchStageSemanticHybrid,
	}})
	result, err := service.Prepare(t.Context(), PrepareRequest{
		WorkspaceSlug: "acorn",
		UserInput:     "fix sqlite query rows",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0: %#v", len(result.Entries), result.Entries)
	}
	if result.SkillTree == nil {
		t.Fatalf("expected skill tree in result")
	}
	if _, ok := result.SkillTree.Categories["sqlite"]; !ok {
		t.Fatalf("expected sqlite category in skill tree")
	}
	sqliteSkill := result.SkillTree.Categories["sqlite"].Skills["sqlite query loop procedure"]
	if sqliteSkill == nil {
		t.Fatalf("expected sqlite skill entry in skill tree: %#v", result.SkillTree.Categories["sqlite"].Skills)
	}
	if got, want := sqliteSkill.RelPath, "skills/learned/sqlite.md"; got != want {
		t.Fatalf("skill relpath = %q, want %q", got, want)
	}
	if !hasProcedureActivation(result.ProcedureActivations, ProcedureActivationMatched, "skills/learned/sqlite.md#sqlite-query-loop-procedure") {
		t.Fatalf("missing matched activation: %#v", result.ProcedureActivations)
	}
}

func TestListSkillsFailsWhenIndexedFileDisappears(t *testing.T) {
	service := newTestService(t)
	writeFile(t, service, "skills/learned/sqlite.md", `---
origin: action_verified
task_pattern: sqlite, query, rows
status: verified
created: 2026-05-08
updated: 2026-05-08
source_run: run_verified
evidence_refs:
  - tool-result:run_verified:call_test
---

# SQLite Query Loop Procedure

Always close rows and check rows.Err.
`)
	if err := os.Remove(filepath.Join(service.Root(), "skills", "learned", "sqlite.md")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err := service.ListSkills(t.Context(), RecordSelection{})
	if err == nil || !strings.Contains(err.Error(), "read indexed skill record") {
		t.Fatalf("ListSkills error = %v, want indexed skill read failure", err)
	}
}

func TestPrepareRejectsLimitAboveExplicitMaximum(t *testing.T) {
	service := newTestService(t)
	_, err := service.Prepare(t.Context(), PrepareRequest{
		UserInput: "lint",
		MaxNudges: maxPrepareNudges + 1,
	})
	if err == nil || !strings.Contains(err.Error(), "prepare nudges limit") {
		t.Fatalf("Prepare error = %v, want explicit nudges limit error", err)
	}
}

func TestAppendHistoryWritesSearchableHistoryRecord(t *testing.T) {
	service := newTestService(t)
	event := HistoryEvent{
		SessionID:    "session one",
		RunID:        "run_abc",
		Status:       "succeeded",
		Summary:      "fixed memory module search",
		FilesChanged: []string{"internal/memorymodule/search.go"},
		Timestamp:    time.Date(2026, 5, 7, 10, 15, 0, 0, time.UTC),
	}
	if err := service.AppendHistory(t.Context(), event); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(service.Root(), "history", "session-one.md"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(string(data), "run_abc succeeded fixed memory module search") {
		t.Fatalf("history content = %q", string(data))
	}
	history, err := service.ListHistory(t.Context(), RecordSelection{})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
	if history[0].Scope != "user" || history[0].Status != StatusVerified {
		t.Fatalf("history scope/status = %q/%q", history[0].Scope, history[0].Status)
	}
	if got := strings.Join(history[0].Tags, ","); got != "history" {
		t.Fatalf("history tags = %q", got)
	}
	if history[0].Created != "2026-05-07" || history[0].Updated != "2026-05-07" || history[0].ValidFrom != "2026-05-07" {
		t.Fatalf("history dates = created:%q updated:%q valid_from:%q", history[0].Created, history[0].Updated, history[0].ValidFrom)
	}
}

func TestBuildMemoryInstruction(t *testing.T) {
	service := newTestService(t)
	instruction, err := service.BuildMemoryInstruction(context.Background(), "acorn")
	if err != nil {
		t.Fatalf("BuildMemoryInstruction: %v", err)
	}
	for _, want := range []string{
		"memory_search",
		"memory_read_file",
		"memory_replace_span",
		"procedure skill",
		"leave memory unchanged",
		"status: unverified",
		"origin: agent_draft",
		"evidence_refs",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("instruction missing %q:\n%s", want, instruction)
		}
	}
}

func newTestService(t *testing.T) *LocalService {
	t.Helper()
	service, err := NewLocalService(Config{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	if err := service.EnsureLayout(t.Context()); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	return service
}

func writeFile(t *testing.T, service *LocalService, rel string, content string) {
	t.Helper()
	path := filepath.Join(service.Root(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex after writeFile: %v", err)
	}
}

func setSemanticSearchHits(t *testing.T, service *LocalService, hits []SemanticHit) {
	t.Helper()
	if err := service.SetSemanticRuntime(SemanticRuntimeOptions{
		Index:      &fakeSearchSemanticIndex{hits: hits},
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
}

func hasProcedureActivation(items []ProcedureActivation, phase ProcedureActivationPhase, ref string) bool {
	for _, item := range items {
		if item.Phase == phase && item.ProcedureRef == ref {
			return true
		}
	}
	return false
}

func assertSearchRefs(t *testing.T, items []SearchItem, expected []string) {
	t.Helper()
	if len(items) < len(expected) {
		t.Fatalf("len(items) = %d, want at least %d: %#v", len(items), len(expected), items)
	}
	for i, want := range expected {
		if items[i].Ref != want {
			t.Fatalf("items[%d].Ref = %q, want %q; items=%#v", i, items[i].Ref, want, items)
		}
	}
}

func assertEntryRefs(t *testing.T, items []Entry, expected []string) {
	t.Helper()
	if len(items) < len(expected) {
		t.Fatalf("len(entries) = %d, want at least %d: %#v", len(items), len(expected), items)
	}
	for i, want := range expected {
		if items[i].Ref != want {
			t.Fatalf("entries[%d].Ref = %q, want %q; entries=%#v", i, items[i].Ref, want, items)
		}
	}
}

func explainHasStage(explain *SearchExplain, stage string) bool {
	if explain == nil {
		return false
	}
	for _, item := range explain.Stages {
		if item.Name == stage {
			return true
		}
	}
	return false
}

func containsSearchRef(items []SearchItem, ref string) bool {
	for _, item := range items {
		if item.Ref == ref {
			return true
		}
	}
	return false
}
