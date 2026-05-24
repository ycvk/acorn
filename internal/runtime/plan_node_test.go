package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolresult"
)

type planNodeModel struct {
	responses      []string
	err            error
	callCount      int
	recordedInputs [][]*schema.Message
}

func (m *planNodeModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	m.recordedInputs = append(m.recordedInputs, append([]*schema.Message(nil), messages...))
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if m.callCount > len(m.responses) {
		return schema.AssistantMessage(`{"steps":[{"id":"s1","action":"fallback","status":"pending"}]}`, nil), nil
	}
	return schema.AssistantMessage(m.responses[m.callCount-1], nil), nil
}

func (m *planNodeModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type fakePlanStore struct {
	loaded    *Plan
	saved     *Plan
	loadErr   error
	saveErr   error
	loadCount int
}

func (s *fakePlanStore) OrchestrationPlanStore() {}

func (s *fakePlanStore) LoadPlan(_ context.Context, _ string) (*Plan, error) {
	s.loadCount++
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.loaded == nil {
		return nil, store.ErrPlanNotFound
	}
	return s.loaded, nil
}

func (s *fakePlanStore) SavePlan(_ context.Context, plan *Plan) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = plan
	s.loaded = plan
	return nil
}

func (s *fakePlanStore) AppendStepEvidence(_ context.Context, _ string, runID string, stepID string, evidence PlanEvidence) (*Plan, error) {
	if s.loaded == nil {
		return nil, fmt.Errorf("plan not loaded")
	}
	for i, step := range s.loaded.Steps {
		if step.ID == stepID {
			s.loaded.Steps[i].Evidence = append(s.loaded.Steps[i].Evidence, evidence)
			s.loaded.RunID = runID
			s.saved = s.loaded
			return s.loaded, nil
		}
	}
	return nil, fmt.Errorf("step %s missing", stepID)
}

func (s *fakePlanStore) AppendToolResultEvidenceRef(_ context.Context, resultRef string, ref toolresult.EvidenceRef) error {
	if s.loaded == nil {
		return fmt.Errorf("plan not loaded")
	}
	for i, step := range s.loaded.Steps {
		for j, evidence := range step.Evidence {
			if evidence.ToolResultRef != strings.TrimSpace(resultRef) {
				continue
			}
			if containsEvidenceRef(s.loaded.Steps[i].Evidence[j].ToolResultRef, ref) {
				return nil
			}
		}
	}
	return nil
}

func containsEvidenceRef(resultRef string, ref toolresult.EvidenceRef) bool {
	return strings.TrimSpace(resultRef) != "" && strings.TrimSpace(ref.Ref) != ""
}

type fakePlanningPromptProvider struct {
	section   string
	err       error
	callCount int
}

func (b *fakePlanningPromptProvider) BuildPlanningPromptSection([]string) (string, error) {
	b.callCount++
	if b.err != nil {
		return "", b.err
	}
	return b.section, nil
}

func TestPlanNodeSavesValidPlan(t *testing.T) {
	model := &planNodeModel{responses: []string{`{
		"steps": [
			{"id": "s1", "action": "Read files", "status": "pending"},
			{"id": "s2", "action": "Write tests", "status": "pending", "depends_on": ["s1"]}
		]
	}`}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan"), "run_plan")
	state := &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("do work")}}

	out, err := node.Invoke(ctx, state)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan == nil {
		t.Fatal("state plan is nil")
	}
	if store.saved == nil {
		t.Fatal("plan was not saved")
	}
	if store.saved.PlanID != "sess_plan" || store.saved.SessionID != "sess_plan" || store.saved.RunID != "run_plan" {
		t.Fatalf("saved plan identity = %+v", store.saved)
	}
	if len(store.saved.Steps) != 2 {
		t.Fatalf("len(saved.Steps) = %d, want 2", len(store.saved.Steps))
	}
	if store.saved.Steps[0].Status != PlanStepPending {
		t.Fatalf("first status = %q, want pending", store.saved.Steps[0].Status)
	}
	if store.saved.Steps[0].Risk != PlanStepRiskRead {
		t.Fatalf("first risk = %q, want read", store.saved.Steps[0].Risk)
	}
	if len(model.recordedInputs) != 1 || model.recordedInputs[0][0].Role != schema.System {
		t.Fatalf("plan prompt was not prepended: %+v", model.recordedInputs)
	}
	prompt := model.recordedInputs[0][0].Content
	for _, want := range []string{
		"internal planning node",
		"Return JSON only",
		`"steps"`,
		"<agent-instructions>",
		"Make a plan",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("plan prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPlanNodeAddsPlanningPromptProviderSectionToModelInput(t *testing.T) {
	model := &planNodeModel{responses: []string{`{"steps":[{"id":"s1","action":"Read runtime plan","status":"pending"}]}`}}
	store := &fakePlanStore{}
	provider := &fakePlanningPromptProvider{section: `{"repo_targets_hint":[{"path":"internal/runtime/plan_types.go","reason":"plan metadata"}],"enabled_tools":["read_file"]}`}
	node := NewPlanNode(model, store, nil, "Make a plan", provider, []string{"read_file"})
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_context"), "run_plan_context")

	if _, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read runtime plan")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if provider.callCount != 1 {
		t.Fatalf("planning prompt callCount = %d, want 1", provider.callCount)
	}
	if len(model.recordedInputs) != 1 || len(model.recordedInputs[0]) == 0 {
		t.Fatalf("model input missing: %+v", model.recordedInputs)
	}
	prompt := model.recordedInputs[0][0].Content
	for _, want := range []string{
		"<planning-context>",
		`"path":"internal/runtime/plan_types.go"`,
		`"read_file"`,
		"repo_targets",
		"verification_intent",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planning prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPlanNodePlanningPromptProviderErrorFailsLoud(t *testing.T) {
	model := &planNodeModel{responses: []string{`{"steps":[{"id":"s1","action":"unused","status":"pending"}]}`}}
	store := &fakePlanStore{}
	provider := &fakePlanningPromptProvider{err: errors.New("planning prompt unavailable")}
	node := NewPlanNode(model, store, nil, "Make a plan", provider, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_context_error"), "run_plan_context_error")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("work")}})
	if err == nil || !strings.Contains(err.Error(), "planning prompt unavailable") {
		t.Fatalf("expected planning prompt error, got %v", err)
	}
	if model.callCount != 0 {
		t.Fatalf("model callCount = %d, want 0", model.callCount)
	}
	if store.saved != nil {
		t.Fatal("plan should not be saved when planning prompt fails")
	}
}

func TestPlanNodeRetriesInvalidPlanFormatOnce(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`not-json`,
		`{"steps":[{"id":"s1","action":"Read README","status":"pending"}]}`,
	}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_retry"), "run_retry")

	if _, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if model.callCount != 2 {
		t.Fatalf("callCount = %d, want 2", model.callCount)
	}
	if len(model.recordedInputs) != 2 {
		t.Fatalf("recordedInputs = %d, want 2", len(model.recordedInputs))
	}
	if got, want := len(model.recordedInputs[1]), len(model.recordedInputs[0])+1; got != want {
		t.Fatalf("retry input length = %d, want %d", got, want)
	}
	repair := model.recordedInputs[1][len(model.recordedInputs[1])-1]
	if repair.Role != schema.User || !strings.Contains(repair.Content, "previous planning response was invalid") || !strings.Contains(repair.Content, "parse plan JSON") {
		t.Fatalf("retry repair message missing parse context: %+v", repair)
	}
}

func TestPlanNodeReusesRunnableExistingPlan(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"new","action":"should not run","status":"pending"}]}`,
	}}
	existing := &Plan{
		PlanID:    "plan_existing",
		SessionID: "sess_existing",
		RunID:     "run_previous",
		Steps: []PlanStep{
			{ID: "s1", Action: "Already done", Status: PlanStepCompleted},
			{ID: "s2", Action: "Continue from here", Status: PlanStepPending, DependsOn: []string{"s1"}},
		},
	}
	store := &fakePlanStore{loaded: existing}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_existing"), "run_continue")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("continue")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan != existing {
		t.Fatalf("reused plan pointer mismatch")
	}
	if model.callCount != 0 {
		t.Fatalf("callCount = %d, want 0", model.callCount)
	}
	if store.saved != nil {
		t.Fatal("existing runnable plan should not be overwritten")
	}
}

func TestPlanNodeReusesRunnableExistingPlanWithoutPlanningPromptProvider(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"new","action":"should not run","status":"pending"}]}`,
	}}
	existing := &Plan{
		PlanID:    "plan_existing",
		SessionID: "sess_existing_context",
		RunID:     "run_previous",
		Steps: []PlanStep{
			{ID: "s1", Action: "Already done", Status: PlanStepCompleted},
			{ID: "s2", Action: "Continue from here", Status: PlanStepPending, DependsOn: []string{"s1"}},
		},
	}
	store := &fakePlanStore{loaded: existing}
	provider := &fakePlanningPromptProvider{err: errors.New("should not load planning prompt")}
	node := NewPlanNode(model, store, nil, "Make a plan", provider, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_existing_context"), "run_continue_context")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("continue")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan != existing {
		t.Fatalf("reused plan pointer mismatch")
	}
	if provider.callCount != 0 {
		t.Fatalf("planning prompt callCount = %d, want 0", provider.callCount)
	}
	if model.callCount != 0 {
		t.Fatalf("model callCount = %d, want 0", model.callCount)
	}
}

func TestPlanNodeRegeneratesOnReplanDecision(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"s1","action":"Try another way","status":"pending"}]}`,
	}}
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_existing",
		SessionID: "sess_replan",
		RunID:     "run_previous",
		Steps: []PlanStep{
			{ID: "old", Action: "Old path", Status: PlanStepFailed},
			{ID: "retry", Action: "Could still run", Status: PlanStepPending},
		},
	}}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_replan"), "run_replan")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{
		Messages:        []*schema.Message{schema.UserMessage("recover")},
		ObserveDecision: graph.ObserveDecision{Decision: graph.ObserveDecisionReplan},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if model.callCount != 1 {
		t.Fatalf("callCount = %d, want 1", model.callCount)
	}
	if out.Plan == nil || len(out.Plan.Steps) != 1 || out.Plan.Steps[0].Action != "Try another way" {
		t.Fatalf("regenerated plan = %+v", out.Plan)
	}
	if store.saved == nil {
		t.Fatal("regenerated plan was not saved")
	}
}

func TestPlanNodeRejectsInvalidPlanAfterRetry(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"s1","action":"one","status":"pending"},{"id":"s1","action":"duplicate","status":"pending"}]}`,
		`{"steps":[{"id":"s1","action":"one","status":"pending"},{"id":"s1","action":"duplicate","status":"pending"}]}`,
	}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_bad"), "run_bad")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("bad")}})
	if err == nil {
		t.Fatal("expected invalid plan error")
	}
	if !strings.Contains(err.Error(), "new plan format") || !strings.Contains(err.Error(), "duplicate step id") {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.callCount != 2 {
		t.Fatalf("callCount = %d, want 2", model.callCount)
	}
	if store.saved != nil {
		t.Fatal("invalid plan should not be saved")
	}
}

func TestPlanNodeRejectsInvalidRepoTargetPath(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"s1","action":"write temp","status":"pending","risk":"write","repo_targets":[{"path":"/tmp/x","reason":"target file","confidence":"high"}],"verification_intent":[{"kind":"test","reason":"prove it"}]}]}`,
		`{"steps":[{"id":"s1","action":"write temp","status":"pending","risk":"write","repo_targets":[{"path":"/tmp/x","reason":"target file","confidence":"high"}],"verification_intent":[{"kind":"test","reason":"prove it"}]}]}`,
	}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_bad_path"), "run_bad_path")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("bad path")}})
	if err == nil || !strings.Contains(err.Error(), "workspace-relative") {
		t.Fatalf("expected workspace-relative error, got %v", err)
	}
}

func TestPlanNodeRejectsInvalidRepoTargetConfidence(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"s1","action":"inspect","status":"pending","repo_targets":[{"path":"README.md","reason":"read it","confidence":"maybe"}]}]}`,
		`{"steps":[{"id":"s1","action":"inspect","status":"pending","repo_targets":[{"path":"README.md","reason":"read it","confidence":"maybe"}]}]}`,
	}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_bad_confidence"), "run_bad_confidence")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("bad confidence")}})
	if err == nil || !strings.Contains(err.Error(), "confidence") {
		t.Fatalf("expected confidence error, got %v", err)
	}
}

func TestPlanNodeRejectsWriteRiskWithoutVerificationIntent(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"s1","action":"edit file","status":"pending","risk":"write","repo_targets":[{"path":"README.md","reason":"edit it","confidence":"high"}]}]}`,
		`{"steps":[{"id":"s1","action":"edit file","status":"pending","risk":"write","repo_targets":[{"path":"README.md","reason":"edit it","confidence":"high"}]}]}`,
	}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_write_no_intent"), "run_write_no_intent")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("edit")}})
	if err == nil || !strings.Contains(err.Error(), "requires verification_intent") {
		t.Fatalf("expected verification_intent error, got %v", err)
	}
}

func TestPlanNodeRejectsUnknownToolHint(t *testing.T) {
	model := &planNodeModel{responses: []string{
		`{"steps":[{"id":"s1","action":"read file","status":"pending","tool_hints":["not_a_tool"]}]}`,
		`{"steps":[{"id":"s1","action":"read file","status":"pending","tool_hints":["not_a_tool"]}]}`,
	}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, []string{"read_file"})
	ctx := withRunID(WithSessionID(context.Background(), "sess_unknown_tool"), "run_unknown_tool")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read")}})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}
}

func TestPlanNodeAcceptsRepoAwareMetadata(t *testing.T) {
	model := &planNodeModel{responses: []string{`{
		"steps": [{
			"id": "s1",
			"action": "Update runtime plan",
			"status": "pending",
			"risk": "write",
			"repo_targets": [{"path":"internal/runtime/plan_types.go","symbol":"PlanStep","reason":"metadata lives here","confidence":"high"}],
			"verification_intent": [{"kind":"test","command":["go","test","./internal/runtime"],"paths":["internal/runtime"],"reason":"runtime plan tests"}],
			"tool_hints": ["read_file"]
		}]
	}`}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, []string{"read_file"})
	ctx := withRunID(WithSessionID(context.Background(), "sess_metadata"), "run_metadata")

	if _, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("metadata")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	step := store.saved.Steps[0]
	if step.Risk != PlanStepRiskWrite {
		t.Fatalf("risk = %q, want write", step.Risk)
	}
	if len(step.RepoTargets) != 1 || step.RepoTargets[0].Path != "internal/runtime/plan_types.go" {
		t.Fatalf("repo targets = %+v", step.RepoTargets)
	}
	if len(step.VerificationIntent) != 1 || step.VerificationIntent[0].Kind != "test" {
		t.Fatalf("verification intent = %+v", step.VerificationIntent)
	}
	if len(step.ToolHints) != 1 || step.ToolHints[0] != "read_file" {
		t.Fatalf("tool hints = %+v", step.ToolHints)
	}
}

func TestPlanNodeAcceptsVerifierVerificationIntent(t *testing.T) {
	model := &planNodeModel{responses: []string{`{
		"steps": [{
			"id": "s1",
			"action": "Ship runtime change",
			"status": "pending",
			"risk": "write",
			"repo_targets": [{"path":"internal/runtime/plan_execute_graph.go","reason":"verifier hookup lives here","confidence":"high"}],
			"verification_intent": [{"kind":"verifier","reason":"independent evidence review required"}],
			"tool_hints": ["read_file"]
		}]
	}`}}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, []string{"read_file"})
	ctx := withRunID(WithSessionID(context.Background(), "sess_verifier_plan"), "run_verifier_plan")

	if _, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("ship change")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	step := store.saved.Steps[0]
	if len(step.VerificationIntent) != 1 || step.VerificationIntent[0].Kind != "verifier" {
		t.Fatalf("verification intent = %+v", step.VerificationIntent)
	}
}

func TestPlanNodeReturnsModelErrorWithoutRetry(t *testing.T) {
	model := &planNodeModel{err: errors.New("provider down")}
	store := &fakePlanStore{}
	node := NewPlanNode(model, store, nil, "Make a plan", nil, nil)
	ctx := withRunID(WithSessionID(context.Background(), "sess_model"), "run_model")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("work")}})
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("expected provider error, got %v", err)
	}
	if model.callCount != 1 {
		t.Fatalf("callCount = %d, want 1", model.callCount)
	}
}
