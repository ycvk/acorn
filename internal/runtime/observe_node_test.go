package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type observeNodeModel struct {
	response  string
	callCount int
}

func (m *observeNodeModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	m.callCount++
	return schema.AssistantMessage(m.response, nil), nil
}

func (m *observeNodeModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func TestObserveNodeReturnsDoneWithoutLLMWhenAllStepsTerminal(t *testing.T) {
	model := &observeNodeModel{response: `{"decision":"replan"}`}
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_observe",
		RunID:     "run_observe",
		Steps: []PlanStep{
			{ID: "s1", Action: "Read", Status: PlanStepCompleted},
			{ID: "s2", Action: "Summarize", Status: PlanStepSkipped},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewObserveNode(model, store)
	ctx := withSessionID(context.Background(), "sess_observe")

	decision, err := node.Decide(ctx, &agentGraphState{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Decision != ObserveDecisionDone {
		t.Fatalf("decision = %q, want done", decision.Decision)
	}
	if model.callCount != 0 {
		t.Fatalf("model callCount = %d, want 0", model.callCount)
	}
}

func TestObserveNodeParsesNextDecision(t *testing.T) {
	model := &observeNodeModel{response: `{"decision":"next","step_id":"s2","reason":"continue"}`}
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_next",
		RunID:     "run_next",
		Steps: []PlanStep{
			{ID: "s1", Action: "Read", Status: PlanStepCompleted},
			{ID: "s2", Action: "Summarize", Status: PlanStepPending, DependsOn: []string{"s1"}},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewObserveNode(model, store)
	ctx := withSessionID(context.Background(), "sess_next")

	decision, err := node.Decide(ctx, &agentGraphState{Messages: []*schema.Message{schema.UserMessage("continue")}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Decision != ObserveDecisionNext || decision.StepID != "s2" {
		t.Fatalf("decision = %+v, want next s2", decision)
	}
	if model.callCount != 1 {
		t.Fatalf("model callCount = %d, want 1", model.callCount)
	}
}

func TestObserveNodeParsesReplanDecision(t *testing.T) {
	model := &observeNodeModel{response: `{"decision":"replan","reason":"tool failed"}`}
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_replan",
		RunID:     "run_replan",
		Steps: []PlanStep{
			{ID: "s1", Action: "Read", Status: PlanStepFailed},
			{ID: "s2", Action: "Try another way", Status: PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewObserveNode(model, store)
	ctx := withSessionID(context.Background(), "sess_replan")

	decision, err := node.Decide(ctx, &agentGraphState{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Decision != ObserveDecisionReplan {
		t.Fatalf("decision = %q, want replan", decision.Decision)
	}
}

func TestObserveNodeRejectsInvalidDecision(t *testing.T) {
	model := &observeNodeModel{response: `{"decision":"next"}`}
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_invalid",
		RunID:     "run_invalid",
		Steps: []PlanStep{
			{ID: "s1", Action: "Read", Status: PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewObserveNode(model, store)
	ctx := withSessionID(context.Background(), "sess_invalid")

	_, err := node.Decide(ctx, &agentGraphState{})
	if err == nil || !strings.Contains(err.Error(), "step_id") {
		t.Fatalf("expected step_id error, got %v", err)
	}
}
