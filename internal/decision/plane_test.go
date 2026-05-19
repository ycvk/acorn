package decision

import (
	"context"
	"testing"
)

func TestBuildHint_InspectFirst(t *testing.T) {
	hint := BuildHint(ActionInspectFirst)
	if hint == nil {
		t.Fatal("expected non-nil hint")
	}
	if hint.ContextPriority != PriorityBalanced {
		t.Fatalf("priority = %q, want %q", hint.ContextPriority, PriorityBalanced)
	}
}

func TestBuildHint_ExecuteWithSkill(t *testing.T) {
	hint := BuildHint(ActionExecuteWithSkill)
	if hint == nil {
		t.Fatal("expected non-nil hint")
	}
	if hint.ContextPriority != PrioritySkill {
		t.Fatalf("priority = %q, want %q", hint.ContextPriority, PrioritySkill)
	}
}

func TestBuildHint_AskUser(t *testing.T) {
	hint := BuildHint(ActionAskUser)
	if hint == nil {
		t.Fatal("expected non-nil hint")
	}
	if hint.ContextPriority != PriorityConversation {
		t.Fatalf("priority = %q, want %q", hint.ContextPriority, PriorityConversation)
	}
}

func TestBuildHint_ExecuteWithoutSkill(t *testing.T) {
	hint := BuildHint(ActionExecuteWithoutSkill)
	if hint == nil {
		t.Fatal("expected non-nil hint")
	}
	if hint.ContextPriority != PriorityBalanced {
		t.Fatalf("priority = %q, want %q", hint.ContextPriority, PriorityBalanced)
	}
}

func TestIsContinuableAction(t *testing.T) {
	for _, action := range []Action{ActionExecuteWithSkill, ActionInspectFirst, ActionExecuteWithoutSkill} {
		if !IsContinuableAction(action) {
			t.Fatalf("expected %q to be continuable", action)
		}
	}
	for _, action := range []Action{ActionAskUser, ActionBlock, ActionResumeRun} {
		if IsContinuableAction(action) {
			t.Fatalf("expected %q to not be continuable", action)
		}
	}
}

func TestPlaneBridge_ProducesFullResult(t *testing.T) {
	profile := DefaultProfile()
	engine := NewEngine(profile)
	bridge := &testPlaneBridge{engine: engine}

	result, err := bridge.Decide(context.Background(), Request{
		RunID: "test-run",
		Input: "debug this error",
		AvailableSkills: []RecommendedSkill{
			{ID: "skill.debug.backend", Name: "Debug Backend", Score: 10, TriggerMatched: true},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Record == nil {
		t.Fatal("expected non-nil record")
	}
	if result.Record.Intent == "" {
		t.Fatal("expected non-empty intent in record")
	}
	if result.Hint == nil {
		t.Fatal("expected non-nil hint")
	}
}

type testPlaneBridge struct {
	engine *Engine
}

func (b *testPlaneBridge) Decide(ctx context.Context, req Request) (*Result, error) {
	record, err := b.engine.Decide(ctx, DecideInput(req))
	if err != nil {
		return nil, err
	}
	selected := selectedSkillResultFromRecordTest(record)
	hint := BuildHint(record.Action)
	return &Result{
		Record:        record,
		SelectedSkill: selected,
		Hint:          hint,
	}, nil
}

func (b *testPlaneBridge) HandleAction(ctx context.Context, req ActionRequest) error {
	return nil
}

func selectedSkillResultFromRecordTest(record *Record) *SelectedSkillResult {
	if record == nil || record.Action != ActionExecuteWithSkill {
		return nil
	}
	return &SelectedSkillResult{
		SkillID:  record.SelectedSkillID,
		Explicit: record.DecisionReason == "explicit_skill",
	}
}
