package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/model"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
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
	testModel := &observeNodeModel{response: `{"decision":"replan"}`}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_observe",
		RunID:     "run_observe",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read", Status: model.PlanStepCompleted},
			{ID: "s2", Action: "Summarize", Status: model.PlanStepSkipped},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := graph.NewObserveNode(testModel, store)
	ctx := runtimeapi.WithSessionID(context.Background(), "sess_observe")

	decision, err := node.Decide(ctx, &graph.AgentGraphState{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Decision != graph.ObserveDecisionDone {
		t.Fatalf("decision = %q, want done", decision.Decision)
	}
	if testModel.callCount != 0 {
		t.Fatalf("model callCount = %d, want 0", testModel.callCount)
	}
}

func TestObserveNodeParsesNextDecision(t *testing.T) {
	testModel := &observeNodeModel{response: `{"decision":"next","step_id":"s2","reason":"continue"}`}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_next",
		RunID:     "run_next",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read", Status: model.PlanStepCompleted},
			{ID: "s2", Action: "Summarize", Status: model.PlanStepPending, DependsOn: []string{"s1"}},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := graph.NewObserveNode(testModel, store)
	ctx := runtimeapi.WithSessionID(context.Background(), "sess_next")

	decision, err := node.Decide(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("continue")}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Decision != graph.ObserveDecisionNext || decision.StepID != "s2" {
		t.Fatalf("decision = %+v, want next s2", decision)
	}
	if testModel.callCount != 1 {
		t.Fatalf("model callCount = %d, want 1", testModel.callCount)
	}
}

func TestObserveNodeParsesReplanDecision(t *testing.T) {
	testModel := &observeNodeModel{response: `{"decision":"replan","reason":"tool failed"}`}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_replan",
		RunID:     "run_replan",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read", Status: model.PlanStepFailed},
			{ID: "s2", Action: "Try another way", Status: model.PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := graph.NewObserveNode(testModel, store)
	ctx := runtimeapi.WithSessionID(context.Background(), "sess_replan")

	decision, err := node.Decide(ctx, &graph.AgentGraphState{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Decision != graph.ObserveDecisionReplan {
		t.Fatalf("decision = %q, want replan", decision.Decision)
	}
}

func TestObserveNodeRejectsInvalidDecision(t *testing.T) {
	testModel := &observeNodeModel{response: `{"decision":"next"}`}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_invalid",
		RunID:     "run_invalid",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read", Status: model.PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := graph.NewObserveNode(testModel, store)
	ctx := runtimeapi.WithSessionID(context.Background(), "sess_invalid")

	_, err := node.Decide(ctx, &graph.AgentGraphState{})
	if err == nil || !strings.Contains(err.Error(), "step_id") {
		t.Fatalf("expected step_id error, got %v", err)
	}
}
