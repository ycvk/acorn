package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

func TestRetrieveCandidatesExplicitSkill(t *testing.T) {
	registry := newSkillSelectionRegistry(t)
	items := []skills.Spec{
		{
			ID:           "skill.inspect.repo",
			Name:         "Inspect Repo",
			Source:       "workspace",
			TriggerHints: []string{"inspect repo"},
			Requires:     skills.Requirements{Tools: []string{"read_file"}},
		},
	}
	eligibility := skillEligibilityContextFromCatalog(registry)
	result, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:           "unrelated input",
		ExplicitSkillID: "skill.inspect.repo",
		Eligibility:     eligibility,
	}, items)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	if got, want := result.Candidates[0].Skill.ID, "skill.inspect.repo"; got != want {
		t.Fatalf("candidate skill id = %q, want %q", got, want)
	}
	if !result.Candidates[0].TriggerMatched {
		t.Fatal("candidate trigger matched = false, want true")
	}
}

func TestRetrieveCandidatesExplicitMissingSkillFails(t *testing.T) {
	registry := newSkillSelectionRegistry(t)
	items := []skills.Spec{
		{ID: "skill.inspect.repo", Name: "Inspect Repo", Source: "workspace"},
	}
	eligibility := skillEligibilityContextFromCatalog(registry)
	result, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:           "inspect repo",
		ExplicitSkillID: "skill.missing",
		Eligibility:     eligibility,
	}, items)
	if err == nil {
		t.Fatal("expected missing explicit skill error")
	}
	if result == nil || len(result.Candidates) != 0 {
		t.Fatalf("candidates = %#v, want empty", result)
	}
	if !strings.Contains(err.Error(), `explicit skill "skill.missing" not found`) {
		t.Fatalf("error = %q, want explicit missing skill", err)
	}
}

func TestRetrieveCandidatesExplicitIneligibleSkillFails(t *testing.T) {
	registry := newSkillSelectionRegistry(t)
	items := []skills.Spec{
		{
			ID:       "skill.ship.patch",
			Name:     "Ship Patch",
			Source:   "workspace",
			Requires: skills.Requirements{Tools: []string{"unavailable_tool"}},
		},
	}
	eligibility := skillEligibilityContextFromCatalog(registry)
	result, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:           "ship patch",
		ExplicitSkillID: "skill.ship.patch",
		Eligibility:     eligibility,
	}, items)
	if err == nil {
		t.Fatal("expected ineligible explicit skill error")
	}
	if got, want := len(result.Candidates), 1; got != want {
		t.Fatalf("candidates = %d, want %d", got, want)
	}
	if result.Candidates[0].FilteredReason == "" {
		t.Fatalf("filtered reason is empty")
	}
	if !strings.Contains(err.Error(), `explicit skill "skill.ship.patch" is ineligible`) {
		t.Fatalf("error = %q, want explicit ineligible skill", err)
	}
}

func TestRetrieveCandidatesTaskPatternForDecision(t *testing.T) {
	registry := newSkillSelectionRegistry(t)
	stableSkills := []skills.Spec{
		{
			ID:           "skill.inspect.repo",
			Name:         "Inspect Repo",
			Source:       skills.WorkspaceScope,
			Origin:       skills.OriginHuman,
			TriggerHints: []string{"inspect repo"},
			Requires:     skills.Requirements{Tools: []string{"read_file"}},
		},
		{
			ID:           "sop.fix-sqlite-query-loop-error-handling",
			Name:         "SQLite Rows Error Handling SOP",
			Source:       skills.WorkspaceScope,
			Origin:       skills.OriginDistilled,
			TaskPattern:  "fix sqlite query loop error handling",
			TriggerHints: []string{"rows.Err sqlite"},
			Requires:     skills.Requirements{Tools: []string{"read_file"}},
		},
	}
	eligibility := skillEligibilityContextFromCatalog(registry)
	result, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:       "please fix sqlite query loop error handling",
		Eligibility: eligibility,
	}, stableSkills)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) == 0 || result.Candidates[0].Skill.ID != "sop.fix-sqlite-query-loop-error-handling" {
		t.Fatalf("top candidate = %#v", result.Candidates)
	}
	if !result.Candidates[0].TriggerMatched {
		t.Fatalf("SOP candidate should be trigger matched: %#v", result.Candidates[0])
	}
	profile := decision.DefaultProfile()
	engine := decision.NewEngine(profile)
	record, err := engine.Decide(context.Background(), decision.DecideInput{
		RunID:             "run_1",
		Input:             "please fix sqlite query loop error handling",
		HasWorkingContext: true,
		AvailableSkills:   recommendedSkillsFromMatches(runtimeMatchesFromRecommendations(result.Candidates)),
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != decision.ActionExecuteWithSkill {
		t.Fatalf("action = %q, want execute_with_skill", record.Action)
	}
	if record.SelectedSkillID != "sop.fix-sqlite-query-loop-error-handling" {
		t.Fatalf("selected skill = %q", record.SelectedSkillID)
	}
}

func TestStableSkillsFromSnapshotPreservesSpecs(t *testing.T) {
	snapshot := &skills.Snapshot{
		Skills: []skills.View{
			{
				Spec: skills.Spec{
					ID:       "skill.inspect.repo",
					Name:     "Inspect Repo",
					Source:   skills.WorkspaceScope,
					Origin:   skills.OriginHuman,
					Summary:  "Inspect a repository",
					Requires: skills.Requirements{Tools: []string{"read_file"}},
				},
				Eligible: true,
			},
			{
				Spec: skills.Spec{
					ID:       "skill.web.browser.research",
					Name:     "Web Browser Research",
					Source:   skills.BuiltinScope,
					Origin:   skills.OriginHuman,
					Summary:  "Search and browse the web",
					Requires: skills.Requirements{Tools: []string{"load_tools"}},
				},
				Eligible:        false,
				DisabledReasons: []string{"missing_required_tools:web_search"},
			},
		},
	}
	items := stableSkillsFromSnapshot(snapshot)
	if got, want := len(items), 2; got != want {
		t.Fatalf("stable skills = %d, want %d", got, want)
	}
	if items[1].ID != "skill.web.browser.research" {
		t.Fatalf("second skill id = %q", items[1].ID)
	}
	if items[1].Summary != "Search and browse the web" {
		t.Fatalf("summary = %q", items[1].Summary)
	}
	if len(items[1].Requires.Tools) != 1 || items[1].Requires.Tools[0] != "load_tools" {
		t.Fatalf("requires.tools = %#v", items[1].Requires.Tools)
	}
}

func TestRetrieveCandidatesWebCapabilityPromptMatchesWebSkill(t *testing.T) {
	registry := newSkillSelectionRegistry(t)
	stableSkills := []skills.Spec{
		{
			ID:           "skill.web.browser.research",
			Name:         "Web Browser Research",
			Source:       skills.BuiltinScope,
			Origin:       skills.OriginHuman,
			Summary:      "Use deferred web tools for public web research and dynamic pages.",
			TriggerHints: []string{"会联网吗", "能联网吗", "帮我查一下", "搜索一下", "打开网站"},
			Requires:     skills.Requirements{Tools: []string{"load_tools"}},
		},
	}
	eligibility := skillEligibilityContextFromCatalog(registry)
	result, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:       "你会联网吗",
		Eligibility: eligibility,
	}, stableSkills)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	if got, want := result.Candidates[0].Skill.ID, "skill.web.browser.research"; got != want {
		t.Fatalf("top candidate = %q, want %q", got, want)
	}
	if !result.Candidates[0].TriggerMatched {
		t.Fatalf("expected trigger matched candidate, got %#v", result.Candidates[0])
	}
}

func newSkillSelectionRegistry(t *testing.T) *tooling.Catalog {
	t.Helper()
	items := []tooling.ToolSpec{
		{
			ToolContract: skillSelectionToolContract("read_file", tooling.ToolCategoryRead, tooling.ResourceScopeWorkspaceFile, tooling.ParallelPolicyReadOnly, tooling.PlanPolicyNone),
			Tool:         mustInferTool(t, "read_file", func(context.Context, map[string]any) (string, error) { return "ok", nil }),
		},
		{
			ToolContract: skillSelectionToolContract("create_file", tooling.ToolCategoryWrite, tooling.ResourceScopeWorkspaceFile, tooling.ParallelPolicyWriteScoped, tooling.PlanPolicyRequireActivePlan),
			Tool:         mustInferTool(t, "create_file", func(context.Context, map[string]any) (string, error) { return "ok", nil }),
		},
		{
			ToolContract: skillSelectionToolContract("run_command", tooling.ToolCategoryExecute, tooling.ResourceScopeWorkspaceCommand, tooling.ParallelPolicyNeverParallel, tooling.PlanPolicyRequireActivePlan),
			Tool:         mustInferTool(t, "run_command", func(context.Context, map[string]any) (string, error) { return "ok", nil }),
		},
	}
	registry, err := tooling.NewCatalog(context.Background(), items)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return registry
}

func skillSelectionToolContract(
	name string,
	category tooling.ToolCategory,
	scope tooling.ResourceScope,
	parallel tooling.ParallelPolicy,
	plan tooling.PlanPolicy,
) tooling.ToolContract {
	execution := tooling.ToolExecutionPolicy{ParallelPolicy: parallel}
	if parallel == tooling.ParallelPolicyWriteScoped {
		execution.PathArg = "path"
	}
	return tooling.ToolContract{
		Name:          name,
		Source:        "local",
		Kind:          tooling.ToolKindNative,
		Category:      category,
		ResourceScope: scope,
		Profiles:      []tooling.ToolProfile{tooling.ToolProfileRun},
		PlanPolicy:    plan,
		FactPolicy:    tooling.FactPolicyAuto,
		Loading:       tooling.EagerLoadingPolicy(),
		Execution:     execution,
		Result:        tooling.InlineResultPolicy(0),
		Boundary:      tooling.ToolResultBoundaryPolicy(),
		Projection:    tooling.ActivityProjectionPolicy(),
	}
}
