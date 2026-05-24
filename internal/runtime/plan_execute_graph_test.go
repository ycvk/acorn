package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/runtime/graph"
)

type stubChildAgentExecutor struct {
	result  *orchestration.ChildAgentResult
	results []*orchestration.ChildAgentResult
	err     error
	errs    []error
	reqs    []orchestration.ChildAgentRequest
}

func (s *stubChildAgentExecutor) Execute(_ context.Context, req orchestration.ChildAgentRequest) (*orchestration.ChildAgentResult, error) {
	callIndex := len(s.reqs)
	s.reqs = append(s.reqs, req)
	if callIndex < len(s.errs) && s.errs[callIndex] != nil {
		return nil, s.errs[callIndex]
	}
	if s.err != nil {
		return nil, s.err
	}
	if callIndex < len(s.results) {
		return s.results[callIndex], nil
	}
	return s.result, nil
}

func TestExecuteDispatchNodeCompletesStepWithChildEvidence(t *testing.T) {
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_plan_execute",
		RunID:     "run_parent",
		Steps: []PlanStep{{
			ID:        "s1",
			Action:    "inspect repository",
			Status:    PlanStepPending,
			ToolHints: []string{"read_file"},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	child := &stubChildAgentExecutor{
		result: &orchestration.ChildAgentResult{
			ChildRunID:     "run_child_1",
			ChildSessionID: "delegate_run_child_1",
			FinalStatus:    "succeeded",
			OutputSummary:  "inspected repository and found the target file",
			Acceptance:     orchestration.ChildAgentAcceptance{Status: "passed"},
		},
	}
	node := NewExecuteDispatchNode(store, nil, child)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_execute"), "run_parent")

	state, err := node.Invoke(ctx, &graph.AgentGraphState{
		Messages: []*schema.Message{schema.UserMessage("inspect the repo")},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(child.reqs) != 1 {
		t.Fatalf("child request count = %d, want 1", len(child.reqs))
	}
	if child.reqs[0].RequestedMode != orchestrationmode.SingleAgent {
		t.Fatalf("RequestedMode = %q, want %q", child.reqs[0].RequestedMode, orchestrationmode.SingleAgent)
	}
	if child.reqs[0].Origin != orchestration.ChildAgentOriginPlanExecute {
		t.Fatalf("Origin = %q, want %q", child.reqs[0].Origin, orchestration.ChildAgentOriginPlanExecute)
	}
	if child.reqs[0].ChildRunMode != orchestration.ChildRunModeFork || len(child.reqs[0].ContextMessages) != 1 {
		t.Fatalf("forked child context = mode:%q messages:%d", child.reqs[0].ChildRunMode, len(child.reqs[0].ContextMessages))
	}
	if child.reqs[0].WorkspaceMode != orchestration.ChildWorkspaceModeWorktree {
		t.Fatalf("WorkspaceMode = %q, want %q", child.reqs[0].WorkspaceMode, orchestration.ChildWorkspaceModeWorktree)
	}
	if !strings.Contains(child.reqs[0].Task, "user-facing result") || strings.Contains(child.reqs[0].Task, "provide a concise completion summary") {
		t.Fatalf("child task should request user-facing output without summary wrapper: %q", child.reqs[0].Task)
	}
	if store.loaded.Steps[0].Status != PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", store.loaded.Steps[0].Status)
	}
	if len(store.loaded.Steps[0].Evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(store.loaded.Steps[0].Evidence))
	}
	ev := store.loaded.Steps[0].Evidence[0]
	if ev.Kind != EvidenceKindSubagent || ev.Status != EvidenceStatusPassed || ev.ChildRunID != "run_child_1" {
		t.Fatalf("unexpected evidence: %+v", ev)
	}
	if state == nil || state.Plan == nil {
		t.Fatal("state plan is nil")
	}
	if len(state.Messages) == 0 || !strings.Contains(state.Messages[len(state.Messages)-1].Content, "Completed step s1") {
		t.Fatalf("unexpected dispatch message: %+v", state.Messages)
	}
}

func TestExecuteDispatchNodeFailsStepWhenChildFails(t *testing.T) {
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_plan_execute",
		RunID:     "run_parent",
		Steps: []PlanStep{{
			ID:     "s1",
			Action: "run tests",
			Status: PlanStepPending,
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	child := &stubChildAgentExecutor{err: errors.New("tool run failed")}
	node := NewExecuteDispatchNode(store, nil, child)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_execute"), "run_parent")

	state, err := node.Invoke(ctx, &graph.AgentGraphState{
		Messages: []*schema.Message{schema.UserMessage("run tests")},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if store.loaded.Steps[0].Status != PlanStepFailed {
		t.Fatalf("step status = %q, want failed", store.loaded.Steps[0].Status)
	}
	if len(store.loaded.Steps[0].Evidence) != 1 || store.loaded.Steps[0].Evidence[0].Status != EvidenceStatusFailed {
		t.Fatalf("unexpected evidence: %+v", store.loaded.Steps[0].Evidence)
	}
	if state == nil || len(state.Messages) == 0 || !strings.Contains(state.Messages[len(state.Messages)-1].Content, "failed") {
		t.Fatalf("unexpected dispatch failure message: %+v", state.Messages)
	}
}

func TestExecuteDispatchNodeRunsVerifierForVerifierIntent(t *testing.T) {
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_plan_execute",
		RunID:     "run_parent",
		Steps: []PlanStep{{
			ID:     "s1",
			Action: "update runtime docs",
			Status: PlanStepPending,
			VerificationIntent: []VerificationIntent{{
				Kind:   "verifier",
				Reason: "independent evidence review required",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	child := &stubChildAgentExecutor{
		results: []*orchestration.ChildAgentResult{
			{
				ChildRunID:     "run_child_execute",
				ChildSessionID: "delegate_run_child_execute",
				FinalStatus:    "succeeded",
				OutputSummary:  "updated runtime docs",
				Acceptance:     orchestration.ChildAgentAcceptance{Status: "passed"},
			},
			{
				ChildRunID:     "run_child_verifier",
				ChildSessionID: "delegate_run_child_verifier",
				FinalStatus:    "succeeded",
				OutputSummary:  `{"verdict":"passed","summary":"evidence supports the doc update","missing_evidence":[],"blocking_findings":[],"next_action":""}`,
				Acceptance:     orchestration.ChildAgentAcceptance{Status: "passed"},
			},
		},
	}
	node := NewExecuteDispatchNode(store, nil, child)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_execute"), "run_parent")

	state, err := node.Invoke(ctx, &graph.AgentGraphState{
		Messages: []*schema.Message{schema.UserMessage("update docs")},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(child.reqs) != 2 {
		t.Fatalf("child request count = %d, want 2", len(child.reqs))
	}
	if child.reqs[1].Origin != orchestration.ChildAgentOriginVerifier {
		t.Fatalf("verifier origin = %q, want verifier", child.reqs[1].Origin)
	}
	if child.reqs[1].ParentStepID != "s1" {
		t.Fatalf("verifier parent step = %q, want s1", child.reqs[1].ParentStepID)
	}
	if strings.Join(child.reqs[1].AllowedToolNames, ",") != "read_file,list_files,search_text,inspect_git_status,inspect_git_diff" {
		t.Fatalf("verifier allowed tools = %+v", child.reqs[1].AllowedToolNames)
	}
	if !strings.Contains(child.reqs[1].Task, "Do not modify the workspace") || !strings.Contains(child.reqs[1].Task, "independent evidence review required") {
		t.Fatalf("verifier task missing read-only criteria:\n%s", child.reqs[1].Task)
	}
	if store.loaded.Steps[0].Status != PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", store.loaded.Steps[0].Status)
	}
	if len(store.loaded.Steps[0].Evidence) != 2 {
		t.Fatalf("evidence count = %d, want 2", len(store.loaded.Steps[0].Evidence))
	}
	verifierEvidence := store.loaded.Steps[0].Evidence[1]
	if verifierEvidence.Kind != EvidenceKindVerifier || verifierEvidence.Status != EvidenceStatusPassed || verifierEvidence.ChildRunID != "run_child_verifier" {
		t.Fatalf("verifier evidence = %+v", verifierEvidence)
	}
	if state == nil || state.Plan == nil {
		t.Fatal("state plan is nil")
	}
}

func TestExecuteDispatchNodeFailsStepWhenVerifierFails(t *testing.T) {
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_plan_execute",
		RunID:     "run_parent",
		Steps: []PlanStep{{
			ID:     "s1",
			Action: "ship runtime change",
			Status: PlanStepPending,
			VerificationIntent: []VerificationIntent{{
				Kind:   "verifier",
				Reason: "independent evidence review required",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	child := &stubChildAgentExecutor{
		results: []*orchestration.ChildAgentResult{
			{
				ChildRunID:     "run_child_execute",
				ChildSessionID: "delegate_run_child_execute",
				FinalStatus:    "succeeded",
				OutputSummary:  "runtime change shipped",
				Acceptance:     orchestration.ChildAgentAcceptance{Status: "passed"},
			},
			{
				ChildRunID:     "run_child_verifier",
				ChildSessionID: "delegate_run_child_verifier",
				FinalStatus:    "succeeded",
				OutputSummary:  `{"verdict":"failed","summary":"missing regression test","missing_evidence":[],"blocking_findings":["missing regression test"],"next_action":"add regression test"}`,
				Acceptance:     orchestration.ChildAgentAcceptance{Status: "passed"},
			},
		},
	}
	node := NewExecuteDispatchNode(store, nil, child)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_execute"), "run_parent")

	state, err := node.Invoke(ctx, &graph.AgentGraphState{
		Messages: []*schema.Message{schema.UserMessage("ship change")},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if store.loaded.Steps[0].Status != PlanStepFailed {
		t.Fatalf("step status = %q, want failed", store.loaded.Steps[0].Status)
	}
	if len(store.loaded.Steps[0].Evidence) != 2 {
		t.Fatalf("evidence count = %d, want 2", len(store.loaded.Steps[0].Evidence))
	}
	verifierEvidence := store.loaded.Steps[0].Evidence[1]
	if verifierEvidence.Kind != EvidenceKindVerifier || verifierEvidence.Status != EvidenceStatusFailed || verifierEvidence.Error != "missing regression test" {
		t.Fatalf("verifier evidence = %+v", verifierEvidence)
	}
	if state == nil || len(state.Messages) == 0 || !strings.Contains(state.Messages[len(state.Messages)-1].Content, "missing regression test") {
		t.Fatalf("unexpected failure message: %+v", state.Messages)
	}
}

func TestExecuteDispatchNodeDoesNotDispatchWithoutRunnableStep(t *testing.T) {
	store := &fakePlanStore{loaded: &Plan{
		PlanID:    "plan_1",
		SessionID: "sess_plan_execute",
		RunID:     "run_parent",
		Steps: []PlanStep{{
			ID:     "s1",
			Action: "already done",
			Status: PlanStepCompleted,
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	child := &stubChildAgentExecutor{
		result: &orchestration.ChildAgentResult{
			ChildRunID:    "run_child_unexpected",
			FinalStatus:   "succeeded",
			OutputSummary: "should not run",
		},
	}
	node := NewExecuteDispatchNode(store, nil, child)
	ctx := withRunID(WithSessionID(context.Background(), "sess_plan_execute"), "run_parent")

	_, err := node.Invoke(ctx, &graph.AgentGraphState{
		Messages: []*schema.Message{schema.UserMessage("continue")},
	})
	if err == nil || !strings.Contains(err.Error(), "active plan has no runnable pending step") {
		t.Fatalf("Invoke err = %v, want no runnable pending step", err)
	}
	if len(child.reqs) != 0 {
		t.Fatalf("child request count = %d, want 0", len(child.reqs))
	}
}

func TestCloseoutNodeProducesHumanReadableSummary(t *testing.T) {
	node := NewCloseoutNode()
	state := &graph.AgentGraphState{
		Plan: &Plan{
			Steps: []PlanStep{
				{
					ID:     "s1",
					Action: "inspect repository",
					Status: PlanStepCompleted,
					Evidence: []PlanEvidence{{
						Summary: "found the target file",
					}},
				},
				{
					ID:     "s2",
					Action: "run integration tests",
					Status: PlanStepFailed,
					Evidence: []PlanEvidence{{
						Kind:   EvidenceKindSubagent,
						Status: EvidenceStatusFailed,
						Error:  "tests timed out",
					}},
				},
			},
		},
	}

	msg, err := node.Invoke(context.Background(), state)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if strings.Contains(msg.Content, "\"child_run_id\"") {
		t.Fatalf("closeout should not emit raw child JSON: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Completed:") || !strings.Contains(msg.Content, "Not completed:") {
		t.Fatalf("unexpected closeout message: %q", msg.Content)
	}
}

func TestCloseoutNodeSingleCompletedStepReturnsChildSummaryOnly(t *testing.T) {
	node := NewCloseoutNode()
	state := &graph.AgentGraphState{
		Plan: &Plan{
			Steps: []PlanStep{{
				ID:     "s1",
				Action: "Respond to the user's greeting in Chinese with a friendly welcome message",
				Status: PlanStepCompleted,
				Evidence: []PlanEvidence{{
					Summary: "你好！欢迎使用 Acorn。我是你的智能助手，很高兴为你服务。",
				}},
			}},
		},
	}

	msg, err := node.Invoke(context.Background(), state)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if msg.Content != "你好！欢迎使用 Acorn。我是你的智能助手，很高兴为你服务。" {
		t.Fatalf("single step closeout = %q", msg.Content)
	}
	if strings.Contains(msg.Content, state.Plan.Steps[0].Action) {
		t.Fatalf("single step closeout leaked internal plan action: %q", msg.Content)
	}
}
