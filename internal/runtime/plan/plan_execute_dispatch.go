package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
)

type ExecuteDispatchNode struct {
	store         runtimeapi.PlanStore
	childExecutor orchestration.ChildAgentExecutor
	verifier      orchestration.Verifier
}

func NewExecuteDispatchNode(store runtimeapi.PlanStore, childExecutor orchestration.ChildAgentExecutor) *ExecuteDispatchNode {
	var verifier orchestration.Verifier
	if childExecutor != nil {
		verifier = orchestration.NewChildAgentVerifier(childExecutor)
	}
	return &ExecuteDispatchNode{
		store:         store,
		childExecutor: childExecutor,
		verifier:      verifier,
	}
}

func (n *ExecuteDispatchNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
	if err := n.validateDispatchState(state); err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	runID := strings.TrimSpace(runtimeapi.GetRunID(ctx))
	if err := validateDispatchIDs(sessionID, runID); err != nil {
		return nil, err
	}
	plan, stepIndex, err := n.loadRunnablePlan(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	plan, stepIndex, err = n.markStepStarted(ctx, plan, stepIndex, runID, sessionID)
	if err != nil {
		return nil, err
	}
	step := plan.Steps[stepIndex]
	if failed, err := n.executeChildAndRecord(ctx, state, sessionID, runID, plan, stepIndex, step); failed || err != nil {
		return state, err
	}
	if failed, err := n.runVerifierIfNeeded(ctx, state, sessionID, runID, plan, stepIndex); failed || err != nil {
		return state, err
	}
	return n.completeDispatchStep(ctx, state, sessionID, runID, plan, stepIndex)
}

func (n *ExecuteDispatchNode) validateDispatchState(state *graph.AgentGraphState) error {
	if state == nil {
		return fmt.Errorf("execute dispatch requires graph state")
	}
	if n == nil || n.store == nil {
		return fmt.Errorf("execute dispatch requires a plan store")
	}
	if n.childExecutor == nil {
		return fmt.Errorf("execute dispatch requires a child executor")
	}
	return nil
}

func validateDispatchIDs(sessionID, runID string) error {
	if sessionID == "" {
		return fmt.Errorf("execute dispatch requires session_id")
	}
	if runID == "" {
		return fmt.Errorf("execute dispatch requires run_id")
	}
	return nil
}

func (n *ExecuteDispatchNode) markStepStarted(ctx context.Context, plan *model.Plan, stepIndex int, runID, sessionID string) (*model.Plan, int, error) {
	if plan.Steps[stepIndex].Status != model.PlanStepPending {
		return plan, stepIndex, nil
	}
	plan.Steps[stepIndex].Status = model.PlanStepInProgress
	plan.RunID = runID
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, -1, fmt.Errorf("mark plan step started: %w", err)
	}
	return plan, stepIndex, nil
}

func (n *ExecuteDispatchNode) executeChildAndRecord(ctx context.Context, state *graph.AgentGraphState, sessionID, runID string, plan *model.Plan, stepIndex int, step model.PlanStep) (bool, error) {
	result, execErr := n.childExecutor.Execute(ctx, n.buildChildRequest(sessionID, runID, plan, step, state.Messages))
	recordedAt := time.Now().UTC()
	evidence := dispatchChildEvidence(step.ID, runID, result, execErr, recordedAt)
	if err := n.recordStepEvidence(ctx, sessionID, runID, step.ID, evidence); err != nil {
		return false, err
	}
	reloadedPlan, reloadedIndex, err := n.reloadStep(ctx, sessionID, step.ID)
	if err != nil {
		return false, err
	}
	*plan = *reloadedPlan
	plan.Steps = reloadedPlan.Steps
	if reason, ok := failedPlanExecutionEvidenceReason(plan.Steps[reloadedIndex].Evidence); ok {
		return true, n.failDispatchStep(ctx, state, plan, reloadedIndex, reason)
	}
	return false, nil
}

func dispatchChildEvidence(stepID, parentRunID string, result *orchestration.ChildAgentResult, execErr error, recordedAt time.Time) model.PlanEvidence {
	if execErr != nil {
		return failedChildExecutionEvidence(stepID, parentRunID, execErr, recordedAt)
	}
	return subagentEvidenceFromChildResult(stepID, parentRunID, result, recordedAt)
}

func (n *ExecuteDispatchNode) recordStepEvidence(ctx context.Context, sessionID, runID, stepID string, evidence model.PlanEvidence) error {
	if _, err := n.store.AppendStepEvidence(ctx, sessionID, runID, stepID, evidence); err != nil {
		return fmt.Errorf("record plan step evidence: %w", err)
	}
	return nil
}

func (n *ExecuteDispatchNode) failDispatchStep(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, reason string) error {
	failedPlan, err := n.failStep(ctx, plan, stepIndex, reason)
	if err != nil {
		return err
	}
	state.Messages = append(state.Messages, schema.AssistantMessage(formatDispatchOutcome(failedPlan.Steps[stepIndex], false), nil))
	state.Plan = failedPlan
	state.Phase = graph.PhaseAct
	return nil
}

func (n *ExecuteDispatchNode) runVerifierIfNeeded(ctx context.Context, state *graph.AgentGraphState, sessionID, runID string, plan *model.Plan, stepIndex int) (bool, error) {
	if !stepRequiresVerifier(plan.Steps[stepIndex]) {
		return false, nil
	}
	if n.verifier == nil {
		return false, fmt.Errorf("execute dispatch requires verifier for verifier intent")
	}
	if err := n.verifyStep(ctx, state, sessionID, runID, plan, stepIndex); err != nil {
		return false, err
	}
	// verifyStep may have failed the step — check if it's still runnable
	reloadedPlan, reloadedIndex, err := n.reloadStep(ctx, sessionID, plan.Steps[stepIndex].ID)
	if err != nil {
		return false, err
	}
	*plan = *reloadedPlan
	if plan.Steps[reloadedIndex].Status == model.PlanStepFailed {
		return true, nil
	}
	return false, nil
}

func (n *ExecuteDispatchNode) verifyStep(ctx context.Context, state *graph.AgentGraphState, sessionID, runID string, plan *model.Plan, stepIndex int) error {
	step := plan.Steps[stepIndex]
	verifyResult, verifyErr := n.verifier.Verify(ctx, n.buildVerificationRequest(sessionID, runID, plan, step, state.Messages))
	recordedAt := time.Now().UTC()
	evidence := dispatchVerifierEvidence(step.ID, runID, verifyResult, verifyErr, recordedAt)
	if err := n.recordStepEvidence(ctx, sessionID, runID, step.ID, evidence); err != nil {
		return fmt.Errorf("record verifier plan step evidence: %w", err)
	}
	reloadedPlan, reloadedIndex, err := n.reloadStep(ctx, sessionID, step.ID)
	if err != nil {
		return err
	}
	plan.Steps = reloadedPlan.Steps
	if reason, ok := failedPlanExecutionEvidenceReason(plan.Steps[reloadedIndex].Evidence); ok {
		return n.failDispatchStep(ctx, state, plan, reloadedIndex, reason)
	}
	return nil
}

func dispatchVerifierEvidence(stepID, parentRunID string, result *orchestration.VerificationResult, verifyErr error, recordedAt time.Time) model.PlanEvidence {
	if verifyErr != nil {
		return failedVerifierExecutionEvidence(stepID, parentRunID, verifyErr, recordedAt)
	}
	return verifierEvidenceFromResult(stepID, parentRunID, result, recordedAt)
}

func (n *ExecuteDispatchNode) completeDispatchStep(ctx context.Context, state *graph.AgentGraphState, sessionID, runID string, plan *model.Plan, stepIndex int) (*graph.AgentGraphState, error) {
	plan.Steps[stepIndex].Status = model.PlanStepCompleted
	plan.RunID = runID
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("mark plan step completed: %w", err)
	}
	state.Messages = append(state.Messages, schema.AssistantMessage(formatDispatchOutcome(plan.Steps[stepIndex], true), nil))
	state.Plan = plan
	state.Phase = graph.PhaseAct
	return state, nil
}

func (n *ExecuteDispatchNode) loadRunnablePlan(ctx context.Context, sessionID string) (*model.Plan, int, error) {
	plan, err := n.store.LoadPlan(ctx, sessionID)
	if err != nil {
		return nil, -1, fmt.Errorf("load active plan: %w", err)
	}
	index, err := graph.FindRunnablePlanStep(plan)
	if err != nil {
		return nil, -1, err
	}
	return plan, index, nil
}

func (n *ExecuteDispatchNode) reloadStep(ctx context.Context, sessionID string, stepID string) (*model.Plan, int, error) {
	plan, err := n.store.LoadPlan(ctx, sessionID)
	if err != nil {
		return nil, -1, fmt.Errorf("reload plan: %w", err)
	}
	for i, step := range plan.Steps {
		if step.ID == stepID {
			return plan, i, nil
		}
	}
	return nil, -1, fmt.Errorf("plan step %s no longer exists", stepID)
}

func (n *ExecuteDispatchNode) failStep(ctx context.Context, plan *model.Plan, stepIndex int, reason string) (*model.Plan, error) {
	plan.Steps[stepIndex].Status = model.PlanStepFailed
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("mark plan step failed: %w", err)
	}
	return plan, nil
}

func (n *ExecuteDispatchNode) buildChildRequest(sessionID, runID string, plan *model.Plan, step model.PlanStep, messages []*schema.Message) orchestration.ChildAgentRequest {
	return orchestration.ChildAgentRequest{
		ParentRunID:      runID,
		ParentSessionID:  sessionID,
		ParentStepID:     step.ID,
		Task:             formatExecuteChildTask(plan, step),
		ChildRunMode:     orchestration.ChildRunModeFork,
		WorkspaceMode:    orchestration.ChildWorkspaceModeWorktree,
		ContextMessages:  append([]*schema.Message(nil), messages...),
		AllowedToolNames: append([]string(nil), step.ToolHints...),
		Origin:           orchestration.ChildAgentOriginPlanExecute,
		RequestedMode:    events.ModeSingleAgent,
	}
}

func (n *ExecuteDispatchNode) buildVerificationRequest(sessionID, runID string, plan *model.Plan, step model.PlanStep, messages []*schema.Message) orchestration.VerificationRequest {
	return orchestration.VerificationRequest{
		ParentRunID:        runID,
		ParentSessionID:    sessionID,
		PlanID:             plan.PlanID,
		StepIDs:            []string{step.ID},
		AcceptanceCriteria: verifierAcceptanceCriteria(step),
		EvidenceRefs:       verifierEvidenceRefs(step.Evidence),
		ToolResultRefs:     verifierToolResultRefs(step.Evidence),
		ContextMessages:    append([]*schema.Message(nil), messages...),
		AllowedToolNames:   verifierReadOnlyToolNames(),
	}
}

func subagentEvidenceFromChildResult(stepID, parentRunID string, result *orchestration.ChildAgentResult, recordedAt time.Time) model.PlanEvidence {
	if result == nil {
		return failedChildExecutionEvidence(stepID, parentRunID, errors.New("child result is nil"), recordedAt)
	}
	summary := summarizeChildResult(result)
	status, errText := childResultAcceptance(result)
	return model.PlanEvidence{
		ID:          fmt.Sprintf("subagent-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        model.EvidenceKindSubagent,
		Status:      status,
		Summary:     summary,
		ChildRunID:  strings.TrimSpace(result.ChildRunID),
		Error:       errText,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func childResultAcceptance(result *orchestration.ChildAgentResult) (model.EvidenceStatus, string) {
	if strings.TrimSpace(result.Acceptance.Status) == "passed" {
		return model.EvidenceStatusPassed, ""
	}
	errText := strings.Join(trimmedNonEmptyStrings(result.Acceptance.Reasons), "; ")
	if errText == "" {
		errText = fmt.Sprintf("child run %s acceptance failed", strings.TrimSpace(result.ChildRunID))
	}
	return model.EvidenceStatusFailed, errText
}

func failedChildExecutionEvidence(stepID, parentRunID string, execErr error, recordedAt time.Time) model.PlanEvidence {
	reason := "child execution failed"
	if execErr != nil && strings.TrimSpace(execErr.Error()) != "" {
		reason = strings.TrimSpace(execErr.Error())
	}
	return model.PlanEvidence{
		ID:          fmt.Sprintf("subagent-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        model.EvidenceKindSubagent,
		Status:      model.EvidenceStatusFailed,
		Summary:     reason,
		Error:       reason,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func failedVerifierExecutionEvidence(stepID, parentRunID string, execErr error, recordedAt time.Time) model.PlanEvidence {
	reason := "verifier execution failed"
	if execErr != nil && strings.TrimSpace(execErr.Error()) != "" {
		reason = strings.TrimSpace(execErr.Error())
	}
	return model.PlanEvidence{
		ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        model.EvidenceKindVerifier,
		Status:      model.EvidenceStatusFailed,
		Summary:     reason,
		Error:       reason,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func summarizeChildResult(result *orchestration.ChildAgentResult) string {
	if result == nil {
		return ""
	}
	if summary := strings.TrimSpace(result.OutputSummary); summary != "" {
		return summary
	}
	if len(result.EvidenceSummaries) > 0 {
		return strings.Join(trimmedNonEmptyStrings(result.EvidenceSummaries), "; ")
	}
	childRunID := strings.TrimSpace(result.ChildRunID)
	if childRunID == "" {
		return "child execution completed"
	}
	if strings.TrimSpace(result.Acceptance.Status) == "passed" {
		return fmt.Sprintf("child run %s completed", childRunID)
	}
	return fmt.Sprintf("child run %s failed", childRunID)
}

func formatDispatchOutcome(step model.PlanStep, succeeded bool) string {
	summary := latestEvidenceSummary(step.Evidence)
	if succeeded {
		return formatDispatchSuccess(step, summary)
	}
	return formatDispatchFailure(step)
}

func formatDispatchSuccess(step model.PlanStep, summary string) string {
	if summary != "" {
		return fmt.Sprintf("Completed step %s: %s", strings.TrimSpace(step.ID), summary)
	}
	return fmt.Sprintf("Completed step %s.", strings.TrimSpace(step.ID))
}

func formatDispatchFailure(step model.PlanStep) string {
	if reason, ok := failedPlanExecutionEvidenceReason(step.Evidence); ok && strings.TrimSpace(reason) != "" {
		return fmt.Sprintf("Step %s failed: %s", strings.TrimSpace(step.ID), strings.TrimSpace(reason))
	}
	return fmt.Sprintf("Step %s failed.", strings.TrimSpace(step.ID))
}
