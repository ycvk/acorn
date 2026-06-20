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

func TestDecide_TopRecommendedSkillSelectsAvailableSkill(t *testing.T) {
	engine := NewEngine(DefaultProfile())

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
	if record.DecisionReason != "top_skill_recommendation" {
		t.Fatalf("reason = %q, want top_skill_recommendation", record.DecisionReason)
	}
}

func TestDecide_ExplicitSkillAvailableExecutesWithSkill(t *testing.T) {
	engine := NewEngine(DefaultProfile())

	record, err := engine.Decide(context.Background(), DecideInput{
		Input:           "do something",
		ExplicitSkillID: "skill.debug.backend",
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
	if record.DecisionReason != "explicit_skill" {
		t.Fatalf("reason = %q, want explicit_skill", record.DecisionReason)
	}
}

func TestDecide_ExplicitSkillUnavailableBlocks(t *testing.T) {
	engine := NewEngine(DefaultProfile())

	record, err := engine.Decide(context.Background(), DecideInput{
		Input:           "do something",
		ExplicitSkillID: "skill.missing",
		AvailableSkills: []RecommendedSkill{
			{ID: "skill.debug.backend", Name: "Debug Backend", Score: 10, TriggerMatched: true},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != ActionBlock {
		t.Fatalf("action = %q, want block", record.Action)
	}
	if record.DecisionReason != "explicit_skill_unavailable" {
		t.Fatalf("reason = %q, want explicit_skill_unavailable", record.DecisionReason)
	}
}

func TestDecide_NoSkillsAndNoWorkingContextUsesMissingContextDefault(t *testing.T) {
	engine := NewEngine(DefaultProfile())

	record, err := engine.Decide(context.Background(), DecideInput{
		Input:             "anything",
		HasWorkingContext: false,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != ActionInspectFirst {
		t.Fatalf("action = %q, want inspect_first", record.Action)
	}
	if record.DecisionReason != "missing_context" {
		t.Fatalf("reason = %q, want missing_context", record.DecisionReason)
	}
}

func TestDecide_NoSkillsButWorkingContextExecutesWithoutSkill(t *testing.T) {
	engine := NewEngine(DefaultProfile())

	record, err := engine.Decide(context.Background(), DecideInput{
		Input:             "anything",
		HasWorkingContext: true,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Action != ActionExecuteWithoutSkill {
		t.Fatalf("action = %q, want execute_without_skill", record.Action)
	}
	if record.DecisionReason != "general_execution" {
		t.Fatalf("reason = %q, want general_execution", record.DecisionReason)
	}
}

func TestProfileValidation_RejectsInvalidAction(t *testing.T) {
	profile := DefaultProfile()
	profile.Defaults.MissingRequiredCapability = Action("invalid_action")
	if err := validateProfile(profile); err == nil {
		t.Fatal("expected validation error for invalid action")
	}
}
