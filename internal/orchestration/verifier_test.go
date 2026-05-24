package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestrationmode"
)

type fakeVerifierChildExecutor struct {
	req    ChildAgentRequest
	result *ChildAgentResult
	err    error
	calls  int
}

func (f *fakeVerifierChildExecutor) Execute(_ context.Context, req ChildAgentRequest) (*ChildAgentResult, error) {
	f.calls++
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestChildAgentVerifierBuildsVerifierChildRequest(t *testing.T) {
	child := &fakeVerifierChildExecutor{
		result: &ChildAgentResult{
			ChildRunID:     "run_child_1",
			ChildSessionID: "delegate_run_child_1",
			FinalStatus:    "succeeded",
			OutputSummary:  `{"verdict":"passed","summary":"all criteria satisfied","missing_evidence":[],"blocking_findings":[],"next_action":""}`,
			Acceptance:     ChildAgentAcceptance{Status: "passed"},
		},
	}
	verifier := NewChildAgentVerifier(child)

	result, err := verifier.Verify(context.Background(), VerificationRequest{
		ParentRunID:        " run_parent ",
		ParentSessionID:    " sess_parent ",
		PlanID:             " plan_1 ",
		StepIDs:            []string{" s1 ", ""},
		AcceptanceCriteria: []string{" tests passed ", "diff reviewed"},
		EvidenceRefs:       []string{" ev_1 "},
		ToolResultRefs:     []string{" tool_result_1 "},
		ProcedureRefs:      []string{" procedure_1 "},
		AllowedToolNames:   []string{" read_file "},
		ContextMessages:    []*schema.Message{schema.UserMessage("context")},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Verdict != VerificationVerdictPassed {
		t.Fatalf("verdict = %q, want passed", result.Verdict)
	}
	if child.calls != 1 {
		t.Fatalf("calls = %d, want 1", child.calls)
	}
	if child.req.Origin != ChildAgentOriginVerifier {
		t.Fatalf("origin = %q, want verifier", child.req.Origin)
	}
	if child.req.RequestedMode != orchestrationmode.SingleAgent {
		t.Fatalf("requested mode = %q, want single_agent", child.req.RequestedMode)
	}
	if child.req.ParentRunID != "run_parent" || child.req.ParentSessionID != "sess_parent" || child.req.ParentStepID != "s1" {
		t.Fatalf("parent metadata not normalized: %+v", child.req)
	}
	if len(child.req.ExpectedEvidence) != 0 {
		t.Fatalf("expected evidence = %+v, want none", child.req.ExpectedEvidence)
	}
	if got := strings.Join(child.req.AcceptanceCriteria, ","); got != "verdict" {
		t.Fatalf("acceptance criteria = %q", got)
	}
	if got := strings.Join(child.req.AllowedToolNames, ","); got != "read_file" {
		t.Fatalf("allowed tools = %q", got)
	}
	for _, want := range []string{
		"Do not modify the workspace",
		"Plan ID: plan_1",
		"- s1",
		"- tests passed",
		"- ev_1",
		"- tool_result_1",
		"- procedure_1",
	} {
		if !strings.Contains(child.req.Task, want) {
			t.Fatalf("task missing %q:\n%s", want, child.req.Task)
		}
	}
	if len(child.req.ContextMessages) != 1 || child.req.ContextMessages[0].Content != "context" {
		t.Fatalf("context messages = %+v", child.req.ContextMessages)
	}
}

func TestChildAgentVerifierMapsFailedAcceptance(t *testing.T) {
	child := &fakeVerifierChildExecutor{
		result: &ChildAgentResult{
			ChildRunID:     "run_child_2",
			ChildSessionID: "delegate_run_child_2",
			FinalStatus:    "succeeded",
			OutputSummary:  `{"verdict":"passed","summary":"looks good","missing_evidence":[],"blocking_findings":[],"next_action":""}`,
			Acceptance:     ChildAgentAcceptance{Status: "failed", Reasons: []string{"missing regression test"}},
		},
	}

	result, err := NewChildAgentVerifier(child).Verify(context.Background(), validVerificationRequest())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Verdict != VerificationVerdictFailed {
		t.Fatalf("verdict = %q, want failed", result.Verdict)
	}
	if got := strings.Join(result.BlockingFindings, ","); got != "missing regression test" {
		t.Fatalf("blocking findings = %q", got)
	}
	if result.NextAction != "address verifier findings" {
		t.Fatalf("next action = %q", result.NextAction)
	}
}

func TestChildAgentVerifierDoesNotPassWhenChildFinalStatusFails(t *testing.T) {
	child := &fakeVerifierChildExecutor{
		result: &ChildAgentResult{
			ChildRunID:     "run_child_4",
			ChildSessionID: "delegate_run_child_4",
			FinalStatus:    "failed",
			OutputSummary:  `{"verdict":"passed","summary":"acceptance looks good but child run failed","missing_evidence":[],"blocking_findings":[],"next_action":""}`,
			Acceptance:     ChildAgentAcceptance{Status: "passed"},
		},
	}

	result, err := NewChildAgentVerifier(child).Verify(context.Background(), validVerificationRequest())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Verdict != VerificationVerdictFailed {
		t.Fatalf("verdict = %q, want failed", result.Verdict)
	}
	if len(result.BlockingFindings) == 0 || !strings.Contains(result.BlockingFindings[0], "failed") {
		t.Fatalf("blocking findings = %+v", result.BlockingFindings)
	}
}

func TestChildAgentVerifierMapsUnclearAcceptanceToInconclusive(t *testing.T) {
	child := &fakeVerifierChildExecutor{
		result: &ChildAgentResult{
			ChildRunID:     "run_child_3",
			ChildSessionID: "delegate_run_child_3",
			FinalStatus:    "succeeded",
			OutputSummary:  `{"verdict":"inconclusive","summary":"unable to inspect evidence","missing_evidence":["tool result ref unavailable"],"blocking_findings":[],"next_action":"provide missing evidence and rerun verifier"}`,
			Acceptance:     ChildAgentAcceptance{Status: "unknown", Reasons: []string{"tool result ref unavailable"}},
		},
	}

	result, err := NewChildAgentVerifier(child).Verify(context.Background(), validVerificationRequest())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Verdict != VerificationVerdictInconclusive {
		t.Fatalf("verdict = %q, want inconclusive", result.Verdict)
	}
	if got := strings.Join(result.MissingEvidence, ","); got != "tool result ref unavailable" {
		t.Fatalf("missing evidence = %q", got)
	}
	if result.NextAction != "provide missing evidence and rerun verifier" {
		t.Fatalf("next action = %q", result.NextAction)
	}
}

func TestChildAgentVerifierMapsNilChildResultToInconclusive(t *testing.T) {
	result, err := NewChildAgentVerifier(&fakeVerifierChildExecutor{}).Verify(context.Background(), validVerificationRequest())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Verdict != VerificationVerdictInconclusive {
		t.Fatalf("verdict = %q, want inconclusive", result.Verdict)
	}
	if len(result.MissingEvidence) == 0 || !strings.Contains(result.MissingEvidence[0], "no result") {
		t.Fatalf("missing evidence = %+v", result.MissingEvidence)
	}
}

func TestChildAgentVerifierValidationAndExecutorErrors(t *testing.T) {
	_, err := NewChildAgentVerifier(nil).Verify(context.Background(), validVerificationRequest())
	if err == nil || !strings.Contains(err.Error(), "child agent executor") {
		t.Fatalf("nil executor error = %v", err)
	}
	_, err = NewChildAgentVerifier(&fakeVerifierChildExecutor{}).Verify(context.Background(), VerificationRequest{
		ParentRunID:     "run_parent",
		ParentSessionID: "sess_parent",
	})
	if err == nil || !strings.Contains(err.Error(), "acceptance criteria") {
		t.Fatalf("missing criteria error = %v", err)
	}
	childErr := errors.New("child failed")
	_, err = NewChildAgentVerifier(&fakeVerifierChildExecutor{err: childErr}).Verify(context.Background(), validVerificationRequest())
	if !errors.Is(err, childErr) {
		t.Fatalf("executor error = %v, want %v", err, childErr)
	}
}

func validVerificationRequest() VerificationRequest {
	return VerificationRequest{
		ParentRunID:        "run_parent",
		ParentSessionID:    "sess_parent",
		AcceptanceCriteria: []string{"criteria satisfied"},
	}
}
