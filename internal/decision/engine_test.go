package decision

import (
	"context"
	"testing"
)

func TestDecide_CapabilityFailureBlocksWhenConfigured(t *testing.T) {
	profile := DefaultProfile()
	profile.Defaults.MissingRequiredCapability = ActionBlock
	engine := NewEngine(profile)

	record, err := engine.Decide(context.Background(), DecideInput{
		RunID: "test-run",
		Input: "debug this error",
		AvailableSkills: []RecommendedSkill{
			{ID: "skill.test", Name: "Test", Score: 10, TriggerMatched: true, FilteredReason: "missing_required_tool"},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != ActionBlock {
		t.Fatalf("record action = %q, want %q", record.Action, ActionBlock)
	}
	if record.DecisionReason != "missing_required_capability" {
		t.Fatalf("decision reason = %q, want missing_required_capability", record.DecisionReason)
	}
}

func TestDecide_ProfileRouteSelectsAvailableSkill(t *testing.T) {
	profile := DefaultProfile()
	engine := NewEngine(profile)

	record, err := engine.Decide(context.Background(), DecideInput{
		Input: "debug this runtime error",
		AvailableSkills: []RecommendedSkill{
			{ID: "skill.debug.backend", Name: "Debug Backend", Score: 10, TriggerMatched: true},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != ActionExecuteWithSkill {
		t.Fatalf("action = %q, want execute_with_skill", record.Action)
	}
	if record.SelectedSkillID != "skill.debug.backend" {
		t.Fatalf("selected skill = %q", record.SelectedSkillID)
	}
	if record.DecisionReason != "profile_route" {
		t.Fatalf("reason = %q, want profile_route", record.DecisionReason)
	}
}

func TestDecide_ProfileRouteFallsBackToTopSkillWhenUnavailable(t *testing.T) {
	profile := DefaultProfile()
	engine := NewEngine(profile)

	record, err := engine.Decide(context.Background(), DecideInput{
		Input: "please fix sqlite query loop error handling",
		AvailableSkills: []RecommendedSkill{
			{
				ID:             "sop.fix-sqlite-query-loop-error-handling",
				Name:           "SQLite Rows Error Handling SOP",
				Score:          10,
				TriggerMatched: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != ActionExecuteWithSkill {
		t.Fatalf("action = %q, want execute_with_skill", record.Action)
	}
	if record.SelectedSkillID != "sop.fix-sqlite-query-loop-error-handling" {
		t.Fatalf("selected skill = %q", record.SelectedSkillID)
	}
	if record.DecisionReason != "top_skill_recommendation" {
		t.Fatalf("reason = %q, want top_skill_recommendation", record.DecisionReason)
	}
}

func TestProfileValidation_RejectsInvalidAction(t *testing.T) {
	profile := DefaultProfile()
	profile.Defaults.MissingRequiredCapability = Action("invalid_action")
	if err := validateProfile(profile); err == nil {
		t.Fatal("expected validation error for invalid action")
	}
}
