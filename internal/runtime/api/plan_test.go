package api

import (
	"testing"
	"time"
)

func TestPlanStepStatusConstants(t *testing.T) {
	cases := []struct {
		got  PlanStepStatus
		want string
	}{
		{PlanStepPending, "pending"},
		{PlanStepInProgress, "in_progress"},
		{PlanStepCompleted, "completed"},
		{PlanStepFailed, "failed"},
		{PlanStepSkipped, "skipped"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Fatalf("status constant = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestPlanStepRiskConstants(t *testing.T) {
	cases := []struct {
		got  PlanStepRisk
		want string
	}{
		{PlanStepRiskRead, "read"},
		{PlanStepRiskWrite, "write"},
		{PlanStepRiskExecute, "execute"},
		{PlanStepRiskDelegate, "delegate"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Fatalf("risk constant = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestPlanRoundTrip(t *testing.T) {
	now := time.Now()
	plan := &Plan{
		PlanID:    "plan_1",
		SessionID: "session_1",
		RunID:     "run_1",
		Steps: []PlanStep{
			{
				ID:        "step_1",
				Action:    "read_file",
				Status:    PlanStepPending,
				Risk:      PlanStepRiskRead,
				DependsOn: []string{},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if plan.PlanID != "plan_1" {
		t.Fatalf("PlanID = %q", plan.PlanID)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("len(Steps) = %d", len(plan.Steps))
	}
	if plan.Steps[0].Status != PlanStepPending {
		t.Fatalf("step status = %q", plan.Steps[0].Status)
	}
}

func TestPlanStepDefaults(t *testing.T) {
	step := PlanStep{
		ID:     "s1",
		Action: "exec",
	}
	if step.Status != "" {
		t.Fatalf("default status should be empty, got %q", step.Status)
	}
	if step.Risk != "" {
		t.Fatalf("default risk should be empty, got %q", step.Risk)
	}
}
