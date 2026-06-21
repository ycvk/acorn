package memorymodule

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	_, err := service.Search(t.Context(), SearchRequest{Query: "anything", Limit: maxSearchLimit + 1})
	if err == nil || !strings.Contains(err.Error(), "search limit") {
		t.Fatalf("Search error = %v, want explicit limit error", err)
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
