package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/skills"
)

// runSelectionAsserter is a minimal helper that invokes resolveRunSelectionByResume
// without wiring a full SkillSelector. resolveRunSelectionByResume only reads
// caps.stableSkills and req.SkillID — it does not touch s.deps — so a nil
// SkillSelector is safe for this path.
func runSelectionByResume(t *testing.T, req RunnerBuildRequest, stableSkills []skills.Spec) (*runSelection, error) {
	t.Helper()
	s := &SkillSelector{}
	caps := &runCapabilities{stableSkills: stableSkills}
	return s.resolveRunSelectionByResume(context.Background(), req, caps)
}

func TestResolveRunSelectionByResumeEmptySkillIDReturnsEmptySelection(t *testing.T) {
	// Resume of a direct_response run: no SkillID, no stable skills → empty selection.
	selection, err := runSelectionByResume(t, RunnerBuildRequest{RunID: "run_resume"}, nil)
	if err != nil {
		t.Fatalf("resolveRunSelectionByResume: %v", err)
	}
	if selection == nil {
		t.Fatal("selection = nil, want non-nil")
	}
	if selection.selectedSkill != nil {
		t.Fatalf("selectedSkill = %v, want nil", selection.selectedSkill)
	}
}

func TestResolveRunSelectionByResumeRecoversExplicitSkill(t *testing.T) {
	// Resume of a skill-bearing run: SkillID persisted in runs.skill_id is
	// passed via req.SkillID; resolveRunSelectionByResume must recover it
	// from stableSkills and mark Explicit=true.
	stable := []skills.Spec{
		{ID: "skill.fix-sqlite", Name: "Fix SQLite", Source: "workspace"},
		{ID: "skill.ship-patch", Name: "Ship Patch", Source: "workspace"},
	}
	selection, err := runSelectionByResume(t, RunnerBuildRequest{
		RunID:   "run_resume_skill",
		SkillID: "skill.fix-sqlite",
	}, stable)
	if err != nil {
		t.Fatalf("resolveRunSelectionByResume: %v", err)
	}
	if selection.selectedSkill == nil {
		t.Fatal("selectedSkill = nil, want recovered skill")
	}
	if got, want := selection.selectedSkill.Skill.ID, "skill.fix-sqlite"; got != want {
		t.Fatalf("selectedSkill.Skill.ID = %q, want %q", got, want)
	}
	if !selection.selectedSkill.Explicit {
		t.Fatal("selectedSkill.Explicit = false, want true (resume of explicit skill)")
	}
}

func TestResolveRunSelectionByResumeErrorsOnMissingSkill(t *testing.T) {
	// Resume references a skill id no longer present in stable skills (e.g.
	// skill was deleted between run interruption and resume). Must fail loud
	// rather than silently dropping the skill.
	stable := []skills.Spec{{ID: "skill.other", Name: "Other", Source: "workspace"}}
	_, err := runSelectionByResume(t, RunnerBuildRequest{
		RunID:   "run_resume_missing",
		SkillID: "skill.deleted",
	}, stable)
	if err == nil {
		t.Fatal("resolveRunSelectionByResume: expected error for missing skill, got nil")
	}
	if !strings.Contains(err.Error(), "skill.deleted") {
		t.Fatalf("error = %v, want it to mention the missing skill id", err)
	}
}

// hasCapabilityFailure + topRecommendedSkill + findEligibleSkillByID are the
// inlined decision helpers. Verify their branch semantics directly so the
// removal of internal/decision does not regress selection behavior.

func TestHasCapabilityFailureDetectsMissingRequiredPrefix(t *testing.T) {
	matches := []SkillMatch{
		{Skill: skills.Spec{ID: "a"}, FilteredReason: "missing_required_tools:read_file"},
		{Skill: skills.Spec{ID: "b"}, FilteredReason: ""},
	}
	if !hasCapabilityFailure(matches) {
		t.Fatal("hasCapabilityFailure = false, want true for missing_required_ prefix")
	}

	if hasCapabilityFailure([]SkillMatch{{Skill: skills.Spec{ID: "a"}, FilteredReason: ""}}) {
		t.Fatal("hasCapabilityFailure = true, want false for clean match")
	}
}

func TestTopRecommendedSkillTieBreakByID(t *testing.T) {
	matches := []SkillMatch{
		{Skill: skills.Spec{ID: "zzz"}, Score: 5, TriggerMatched: true},
		{Skill: skills.Spec{ID: "aaa"}, Score: 5, TriggerMatched: true},
	}
	top, ok := topRecommendedSkill(matches)
	if !ok {
		t.Fatal("topRecommendedSkill returned false, want a match")
	}
	if top.Skill.ID != "aaa" {
		t.Fatalf("top skill = %q, want aaa (lexicographic tie-break)", top.Skill.ID)
	}
}

func TestTopRecommendedSkillReturnsFalseWhenNoneEligible(t *testing.T) {
	matches := []SkillMatch{
		{Skill: skills.Spec{ID: "a"}, Score: 0, TriggerMatched: true},
		{Skill: skills.Spec{ID: "b"}, Score: 5, TriggerMatched: false},
		{Skill: skills.Spec{ID: "c"}, Score: 5, FilteredReason: "missing_required_tools:x"},
	}
	if _, ok := topRecommendedSkill(matches); ok {
		t.Fatal("topRecommendedSkill returned true, want false when no eligible match")
	}
}

func TestFindEligibleSkillByIDRequiresEligibility(t *testing.T) {
	matches := []SkillMatch{
		{Skill: skills.Spec{ID: "skill.x"}, Score: 5, TriggerMatched: false},
	}
	if _, ok := findEligibleSkillByID(matches, "skill.x"); ok {
		t.Fatal("findEligibleSkillByID returned true for ineligible match (TriggerMatched=false)")
	}

	matches[0].TriggerMatched = true
	if _, ok := findEligibleSkillByID(matches, "skill.x"); !ok {
		t.Fatal("findEligibleSkillByID returned false for eligible match")
	}

	if _, ok := findEligibleSkillByID(matches, "  "); ok {
		t.Fatal("findEligibleSkillByID returned true for empty/whitespace id")
	}
}

func TestSelectedSkillFromMatchPrefersStableSpec(t *testing.T) {
	match := SkillMatch{
		Skill:        skills.Spec{ID: "skill.x", Name: "Dynamic", Source: "runtime"},
		Score:        7,
		MatchedTerms: []string{"foo", "bar"},
	}
	stable := []skills.Spec{
		{ID: "skill.y", Name: "Other"},
		{ID: "skill.x", Name: "Stable", Source: "workspace", Path: "/skills/x"},
	}
	selected := selectedSkillFromMatch(match, stable, false)
	if selected.Skill.Name != "Stable" {
		t.Fatalf("selected skill name = %q, want Stable (from stableSkills)", selected.Skill.Name)
	}
	if selected.Skill.Path != "/skills/x" {
		t.Fatalf("selected skill path = %q, want /skills/x", selected.Skill.Path)
	}
	if selected.Score != 7 {
		t.Fatalf("selected score = %d, want 7", selected.Score)
	}
	if len(selected.MatchedTerms) != 2 {
		t.Fatalf("matched terms len = %d, want 2", len(selected.MatchedTerms))
	}
}

func TestSelectedSkillFromMatchFallbackWhenNotInStable(t *testing.T) {
	match := SkillMatch{
		Skill: skills.Spec{ID: "skill.dynamic", Name: "Dynamic", Source: "runtime"},
		Score: 3,
	}
	selected := selectedSkillFromMatch(match, nil, true)
	if selected.Skill.ID != "skill.dynamic" {
		t.Fatalf("selected skill id = %q, want skill.dynamic", selected.Skill.ID)
	}
	if !selected.Explicit {
		t.Fatal("selected.Explicit = false, want true")
	}
}

// Ensure resolveRunSelection guards against nil caps.
func TestResolveRunSelectionRejectsNilCaps(t *testing.T) {
	s := &SkillSelector{}
	_, err := s.resolveRunSelection(context.Background(), RunnerBuildRequest{Input: "x"}, nil)
	if err == nil {
		t.Fatal("resolveRunSelection: expected error for nil caps")
	}
	if !strings.Contains(err.Error(), "capabilities") {
		t.Fatalf("error = %v, want capabilities error", err)
	}
}
