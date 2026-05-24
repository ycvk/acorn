package graph

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestGraphPhaseConstants(t *testing.T) {
	cases := []struct {
		got  GraphPhase
		want string
	}{
		{PhasePlan, "plan"},
		{PhaseAct, "act"},
		{PhaseObserve, "observe"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Fatalf("phase = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestObserveDecisionTypeConstants(t *testing.T) {
	cases := []struct {
		got  ObserveDecisionType
		want string
	}{
		{ObserveDecisionNext, "next"},
		{ObserveDecisionReplan, "replan"},
		{ObserveDecisionDone, "done"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Fatalf("decision = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestAgentGraphStateDefaults(t *testing.T) {
	state := AgentGraphState{
		Messages:            []*schema.Message{},
		Plan:                nil,
		ObserveDecision:     ObserveDecision{},
		RemainingIterations: 10,
		AgentName:           "test",
		Phase:               PhasePlan,
	}

	if state.Phase != PhasePlan {
		t.Fatalf("Phase = %q", state.Phase)
	}
	if state.RemainingIterations != 10 {
		t.Fatalf("RemainingIterations = %d", state.RemainingIterations)
	}
	if state.AgentName != "test" {
		t.Fatalf("AgentName = %q", state.AgentName)
	}
}

func TestObserveDecisionStructure(t *testing.T) {
	decision := ObserveDecision{
		Decision: ObserveDecisionDone,
		StepID:   "step_1",
		Reason:   "all tasks completed",
	}
	if decision.Decision != ObserveDecisionDone {
		t.Fatalf("Decision = %q", decision.Decision)
	}
}
