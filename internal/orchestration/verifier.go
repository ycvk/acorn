package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/events"
)

type VerificationVerdict string

const (
	VerificationVerdictPassed       VerificationVerdict = "passed"
	VerificationVerdictFailed       VerificationVerdict = "failed"
	VerificationVerdictInconclusive VerificationVerdict = "inconclusive"
)

type VerificationRequest struct {
	ParentRunID        string
	ParentSessionID    string
	PlanID             string
	StepIDs            []string
	AcceptanceCriteria []string
	EvidenceRefs       []string
	ToolResultRefs     []string
	ProcedureRefs      []string
	ContextMessages    []*schema.Message
	AllowedToolNames   []string
}

type VerificationResult struct {
	ChildRunID       string               `json:"child_run_id"`
	ChildSessionID   string               `json:"child_session_id"`
	Verdict          VerificationVerdict  `json:"verdict"`
	Summary          string               `json:"summary,omitempty"`
	MissingEvidence  []string             `json:"missing_evidence,omitempty"`
	BlockingFindings []string             `json:"blocking_findings,omitempty"`
	NextAction       string               `json:"next_action,omitempty"`
	Acceptance       ChildAgentAcceptance `json:"acceptance"`
}

type Verifier interface {
	Verify(context.Context, VerificationRequest) (*VerificationResult, error)
}

type ChildAgentVerifier struct {
	exec ChildAgentExecutor
}

func NewChildAgentVerifier(exec ChildAgentExecutor) *ChildAgentVerifier {
	return &ChildAgentVerifier{exec: exec}
}

func (v *ChildAgentVerifier) Verify(ctx context.Context, req VerificationRequest) (*VerificationResult, error) {
	if v == nil || v.exec == nil {
		return nil, errors.New("verifier requires child agent executor")
	}
	req = normalizeVerificationRequest(req)
	if req.ParentRunID == "" {
		return nil, errors.New("verification parent run id is required")
	}
	if req.ParentSessionID == "" {
		return nil, errors.New("verification parent session id is required")
	}
	if len(req.AcceptanceCriteria) == 0 {
		return nil, errors.New("verification acceptance criteria are required")
	}
	parentStepID := ""
	if len(req.StepIDs) == 1 {
		parentStepID = req.StepIDs[0]
	}
	childResult, err := v.exec.Execute(ctx, ChildAgentRequest{
		ParentRunID:        req.ParentRunID,
		ParentSessionID:    req.ParentSessionID,
		ParentStepID:       parentStepID,
		Task:               buildVerificationTask(req),
		ContextMessages:    append([]*schema.Message(nil), req.ContextMessages...),
		AllowedToolNames:   append([]string(nil), req.AllowedToolNames...),
		AcceptanceCriteria: []string{"verdict"},
		Origin:             ChildAgentOriginVerifier,
		RequestedMode:      events.ModeSingleAgent,
	})
	if err != nil {
		return nil, err
	}
	return verificationResultFromChild(childResult), nil
}

func normalizeVerificationRequest(req VerificationRequest) VerificationRequest {
	req.ParentRunID = strings.TrimSpace(req.ParentRunID)
	req.ParentSessionID = strings.TrimSpace(req.ParentSessionID)
	req.PlanID = strings.TrimSpace(req.PlanID)
	req.StepIDs = trimList(req.StepIDs)
	req.AcceptanceCriteria = trimList(req.AcceptanceCriteria)
	req.EvidenceRefs = trimList(req.EvidenceRefs)
	req.ToolResultRefs = trimList(req.ToolResultRefs)
	req.ProcedureRefs = trimList(req.ProcedureRefs)
	req.AllowedToolNames = trimList(req.AllowedToolNames)
	return req
}

func buildVerificationTask(req VerificationRequest) string {
	var b strings.Builder
	b.WriteString("Verify the completed work against the acceptance criteria.\n")
	b.WriteString("Do not modify the workspace. Inspect the provided evidence and report only a verification verdict.\n")
	if req.PlanID != "" {
		b.WriteString("\nPlan ID: ")
		b.WriteString(req.PlanID)
		b.WriteString("\n")
	}
	writeList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString("\n")
		b.WriteString(title)
		b.WriteString(":\n")
		for _, item := range items {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	writeList("Step IDs", req.StepIDs)
	writeList("Acceptance criteria", req.AcceptanceCriteria)
	writeList("Evidence refs", req.EvidenceRefs)
	writeList("Tool result refs", req.ToolResultRefs)
	writeList("Procedure refs", req.ProcedureRefs)
	b.WriteString("\nReturn JSON only with this shape: {\"verdict\":\"passed|failed|inconclusive\",\"summary\":\"...\",\"missing_evidence\":[],\"blocking_findings\":[],\"next_action\":\"...\"}.")
	return strings.TrimSpace(b.String())
}

func verificationResultFromChild(child *ChildAgentResult) *VerificationResult {
	if child == nil {
		return &VerificationResult{
			Verdict:         VerificationVerdictInconclusive,
			MissingEvidence: []string{"child verifier returned no result"},
			NextAction:      "rerun verifier with available evidence",
		}
	}
	finalStatus := strings.TrimSpace(child.FinalStatus)
	payload, parseErr := parseVerifierPayload(child.OutputSummary)
	result := &VerificationResult{
		ChildRunID:     strings.TrimSpace(child.ChildRunID),
		ChildSessionID: strings.TrimSpace(child.ChildSessionID),
		Acceptance:     child.Acceptance,
	}
	if parseErr != nil {
		result.Verdict = VerificationVerdictInconclusive
		result.Summary = "verifier did not return a structured verdict"
		result.MissingEvidence = append(trimList(child.Acceptance.Reasons), parseErr.Error())
		result.NextAction = "rerun verifier and require structured JSON verdict"
		if finalStatus != "" && finalStatus != "succeeded" {
			result.Verdict = VerificationVerdictFailed
			result.BlockingFindings = append(result.BlockingFindings, fmt.Sprintf("child run finished with status %s", finalStatus))
			result.NextAction = "address verifier findings"
		}
		return result
	}
	result.Verdict = payload.Verdict
	result.Summary = payload.Summary
	result.MissingEvidence = append([]string(nil), payload.MissingEvidence...)
	result.BlockingFindings = append([]string(nil), payload.BlockingFindings...)
	result.NextAction = payload.NextAction
	if finalStatus != "" && finalStatus != "succeeded" {
		result.Verdict = VerificationVerdictFailed
		result.BlockingFindings = append(result.BlockingFindings, fmt.Sprintf("child run finished with status %s", finalStatus))
		result.NextAction = "address verifier findings"
	}
	if strings.TrimSpace(child.Acceptance.Status) == "failed" {
		result.Verdict = VerificationVerdictFailed
		result.BlockingFindings = append(result.BlockingFindings, trimList(child.Acceptance.Reasons)...)
		result.NextAction = "address verifier findings"
	}
	if result.Verdict == VerificationVerdictFailed && len(result.BlockingFindings) == 0 {
		result.BlockingFindings = []string{"verifier failed"}
	}
	if result.Verdict == VerificationVerdictInconclusive && len(result.MissingEvidence) == 0 {
		result.MissingEvidence = []string{"verifier returned inconclusive without missing evidence"}
		result.NextAction = "provide missing evidence and rerun verifier"
	}
	return result
}

type verifierPayload struct {
	Verdict          VerificationVerdict `json:"verdict"`
	Summary          string              `json:"summary"`
	MissingEvidence  []string            `json:"missing_evidence"`
	BlockingFindings []string            `json:"blocking_findings"`
	NextAction       string              `json:"next_action"`
}

func parseVerifierPayload(output string) (verifierPayload, error) {
	var payload verifierPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload); err != nil {
		return payload, fmt.Errorf("parse verifier verdict: %w", err)
	}
	payload.Verdict = VerificationVerdict(strings.TrimSpace(string(payload.Verdict)))
	payload.Summary = strings.TrimSpace(payload.Summary)
	payload.MissingEvidence = trimList(payload.MissingEvidence)
	payload.BlockingFindings = trimList(payload.BlockingFindings)
	payload.NextAction = strings.TrimSpace(payload.NextAction)
	switch payload.Verdict {
	case VerificationVerdictPassed, VerificationVerdictFailed, VerificationVerdictInconclusive:
	default:
		return payload, fmt.Errorf("verifier verdict %q is invalid", strings.TrimSpace(string(payload.Verdict)))
	}
	return payload, nil
}

func trimList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
