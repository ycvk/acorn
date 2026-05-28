package plan

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/tooling"
)

type ExecuteDispatchNode struct {
	store         runtimeapi.PlanStore
	eventStore    runtimeapi.EventAppender
	childExecutor orchestration.ChildAgentExecutor
	verifier      orchestration.Verifier
}

type CloseoutNode struct{}

func BuildPlanExecuteGraph(
	ctx context.Context,
	agentName string,
	chatModel einomodel.BaseChatModel,
	maxIterations int,
	checkpointStore compose.CheckPointStore,
	handlers []adk.ChatModelAgentMiddleware,
	planStore runtimeapi.PlanStore,
	planPrompt string,
	planningPromptProvider PlanningPromptProvider,
	eagerToolNames []string,
	toolSpecs []tooling.ToolSpec,
	childExecutor orchestration.ChildAgentExecutor,
) (compose.Runnable[*graph.AgentGraphInput, *schema.Message], error) {
	if chatModel == nil {
		return nil, errors.New("plan-execute graph requires a chat model")
	}
	if planStore == nil {
		return nil, errors.New("plan-execute graph requires a plan store")
	}
	if childExecutor == nil {
		return nil, errors.New("plan-execute graph requires a child executor")
	}

	const (
		initNode            = "Init"
		planNode            = "Plan"
		executeDispatchNode = "ExecuteDispatch"
		observeNode         = "Observe"
		closeoutNode        = "Closeout"
	)

	maxIter := maxIterations
	if maxIter <= 0 {
		maxIter = 20
	}

	g := compose.NewGraph[*graph.AgentGraphInput, *schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *graph.AgentGraphState {
			return &graph.AgentGraphState{
				AgentName:           agentName,
				RemainingIterations: maxIter,
			}
		}),
	)

	initLambda := compose.InvokableLambda(func(ctx context.Context, input *graph.AgentGraphInput) (*graph.AgentGraphState, error) {
		state := &graph.AgentGraphState{
			Messages:            append([]*schema.Message(nil), input.Messages...),
			RemainingIterations: maxIter,
			AgentName:           agentName,
		}
		return state, nil
	})
	if err := g.AddLambdaNode(initNode, initLambda, compose.WithNodeName(initNode)); err != nil {
		return nil, fmt.Errorf("add init node: %w", err)
	}

	wrappedModel := chatModel
	if len(handlers) > 0 {
		var err error
		wrappedModel, err = WrapModelWithHandlers(ctx, chatModel, handlers)
		if err != nil {
			return nil, err
		}
	}
	eventStore := eventAppenderFromCheckpointStore(checkpointStore)
	plan := NewPlanNode(wrappedModel, planStore, eventStore, planPrompt, planningPromptProvider, enabledPlanToolNamesFromSpecs(toolSpecs))
	dispatch := NewExecuteDispatchNode(planStore, eventStore, childExecutor)
	observe := graph.NewObserveNode(wrappedModel, planStore)
	closeout := NewCloseoutNode()

	if err := g.AddLambdaNode(planNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		if err := consumePlanIteration(state, maxIter); err != nil {
			return nil, err
		}
		return plan.Invoke(ctx, state)
	}), compose.WithNodeName(planNode)); err != nil {
		return nil, fmt.Errorf("add plan node: %w", err)
	}

	if err := g.AddLambdaNode(executeDispatchNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		return dispatch.Invoke(ctx, state)
	}), compose.WithNodeName(executeDispatchNode)); err != nil {
		return nil, fmt.Errorf("add execute dispatch node: %w", err)
	}

	if err := g.AddLambdaNode(observeNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		decision, err := observe.Decide(ctx, state)
		if err != nil {
			return nil, err
		}
		state.ObserveDecision = decision
		state.Phase = graph.PhaseObserve
		return state, nil
	}), compose.WithNodeName(observeNode)); err != nil {
		return nil, fmt.Errorf("add observe node: %w", err)
	}

	if err := g.AddLambdaNode(closeoutNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*schema.Message, error) {
		return closeout.Invoke(ctx, state)
	}), compose.WithNodeName(closeoutNode)); err != nil {
		return nil, fmt.Errorf("add closeout node: %w", err)
	}

	if err := g.AddEdge(compose.START, initNode); err != nil {
		return nil, fmt.Errorf("add start→init edge: %w", err)
	}
	if err := g.AddEdge(initNode, planNode); err != nil {
		return nil, fmt.Errorf("add init→plan edge: %w", err)
	}
	if err := g.AddEdge(planNode, executeDispatchNode); err != nil {
		return nil, fmt.Errorf("add plan→execute dispatch edge: %w", err)
	}
	if err := g.AddEdge(executeDispatchNode, observeNode); err != nil {
		return nil, fmt.Errorf("add execute dispatch→observe edge: %w", err)
	}

	observeBranch := compose.NewGraphBranch(func(ctx context.Context, state *graph.AgentGraphState) (string, error) {
		switch state.ObserveDecision.Decision {
		case graph.ObserveDecisionNext:
			return executeDispatchNode, nil
		case graph.ObserveDecisionReplan:
			return planNode, nil
		case graph.ObserveDecisionDone:
			return closeoutNode, nil
		default:
			return "", fmt.Errorf("unknown observe decision %q", state.ObserveDecision.Decision)
		}
	}, map[string]bool{executeDispatchNode: true, planNode: true, closeoutNode: true})
	if err := g.AddBranch(observeNode, observeBranch); err != nil {
		return nil, fmt.Errorf("add observe branch: %w", err)
	}
	if err := g.AddEdge(closeoutNode, compose.END); err != nil {
		return nil, fmt.Errorf("add closeout→end edge: %w", err)
	}

	compileOpts := []compose.GraphCompileOption{
		compose.WithGraphName(agentName + "_plan_execute"),
		compose.WithMaxRunSteps(math.MaxInt),
	}
	if !isNilCheckpointStore(checkpointStore) {
		compileOpts = append(compileOpts,
			compose.WithCheckPointStore(checkpointStore),
			compose.WithSerializer(&runtimeapi.JSONSerializer{}),
		)
	}

	runnable, err := g.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, fmt.Errorf("compile plan-execute graph: %w", err)
	}
	return runnable, nil
}

func NewExecuteDispatchNode(store runtimeapi.PlanStore, eventStore runtimeapi.EventAppender, childExecutor orchestration.ChildAgentExecutor) *ExecuteDispatchNode {
	var verifier orchestration.Verifier
	if childExecutor != nil {
		verifier = orchestration.NewChildAgentVerifier(childExecutor)
	}
	return &ExecuteDispatchNode{
		store:         store,
		eventStore:    eventStore,
		childExecutor: childExecutor,
		verifier:      verifier,
	}
}

func (n *ExecuteDispatchNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
	if state == nil {
		return nil, fmt.Errorf("execute dispatch requires graph state")
	}
	if n == nil || n.store == nil {
		return nil, fmt.Errorf("execute dispatch requires a plan store")
	}
	if n.childExecutor == nil {
		return nil, fmt.Errorf("execute dispatch requires a child executor")
	}
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, fmt.Errorf("execute dispatch requires session_id")
	}
	runID := strings.TrimSpace(runtimeapi.GetRunID(ctx))
	if runID == "" {
		return nil, fmt.Errorf("execute dispatch requires run_id")
	}

	plan, stepIndex, err := n.loadRunnablePlan(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if plan.Steps[stepIndex].Status == model.PlanStepPending {
		plan.Steps[stepIndex].Status = model.PlanStepInProgress
		plan.RunID = runID
		plan.UpdatedAt = time.Now().UTC()
		if err := n.store.SavePlan(ctx, plan); err != nil {
			return nil, fmt.Errorf("mark plan step started: %w", err)
		}
		if err := n.emitStepStarted(ctx, plan, plan.Steps[stepIndex]); err != nil {
			return nil, err
		}
	}

	step := plan.Steps[stepIndex]
	result, execErr := n.childExecutor.Execute(ctx, n.buildChildRequest(sessionID, runID, plan, step, state.Messages))
	recordedAt := time.Now().UTC()
	var evidence model.PlanEvidence
	if execErr != nil {
		evidence = failedChildExecutionEvidence(step.ID, runID, execErr, recordedAt)
	} else {
		evidence = subagentEvidenceFromChildResult(step.ID, runID, result, recordedAt)
	}
	if _, err := n.store.AppendStepEvidence(ctx, sessionID, runID, step.ID, evidence); err != nil {
		return nil, fmt.Errorf("record plan step evidence: %w", err)
	}
	plan, stepIndex, err = n.reloadStep(ctx, sessionID, step.ID)
	if err != nil {
		return nil, err
	}
	if reason, ok := failedPlanExecutionEvidenceReason(plan.Steps[stepIndex].Evidence); ok {
		plan, err = n.failStep(ctx, plan, stepIndex, reason)
		if err != nil {
			return nil, err
		}
		state.Messages = append(state.Messages, schema.AssistantMessage(formatDispatchOutcome(plan.Steps[stepIndex], false), nil))
		state.Plan = plan
		state.Phase = graph.PhaseAct
		return state, nil
	}

	if stepRequiresVerifier(plan.Steps[stepIndex]) {
		if n.verifier == nil {
			return nil, fmt.Errorf("execute dispatch requires verifier for verifier intent")
		}
		verifyResult, verifyErr := n.verifier.Verify(ctx, n.buildVerificationRequest(sessionID, runID, plan, plan.Steps[stepIndex], state.Messages))
		recordedAt = time.Now().UTC()
		if verifyErr != nil {
			evidence = failedVerifierExecutionEvidence(plan.Steps[stepIndex].ID, runID, verifyErr, recordedAt)
		} else {
			evidence = verifierEvidenceFromResult(plan.Steps[stepIndex].ID, runID, verifyResult, recordedAt)
		}
		if _, err := n.store.AppendStepEvidence(ctx, sessionID, runID, plan.Steps[stepIndex].ID, evidence); err != nil {
			return nil, fmt.Errorf("record verifier plan step evidence: %w", err)
		}
		plan, stepIndex, err = n.reloadStep(ctx, sessionID, plan.Steps[stepIndex].ID)
		if err != nil {
			return nil, err
		}
		if reason, ok := failedPlanExecutionEvidenceReason(plan.Steps[stepIndex].Evidence); ok {
			plan, err = n.failStep(ctx, plan, stepIndex, reason)
			if err != nil {
				return nil, err
			}
			state.Messages = append(state.Messages, schema.AssistantMessage(formatDispatchOutcome(plan.Steps[stepIndex], false), nil))
			state.Plan = plan
			state.Phase = graph.PhaseAct
			return state, nil
		}
	}

	plan.Steps[stepIndex].Status = model.PlanStepCompleted
	plan.RunID = runID
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("mark plan step completed: %w", err)
	}
	if err := n.emitStepCompleted(ctx, plan, plan.Steps[stepIndex]); err != nil {
		return nil, err
	}
	state.Messages = append(state.Messages, schema.AssistantMessage(formatDispatchOutcome(plan.Steps[stepIndex], true), nil))
	state.Plan = plan
	state.Phase = graph.PhaseAct
	return state, nil
}

func NewCloseoutNode() *CloseoutNode {
	return &CloseoutNode{}
}

func (n *CloseoutNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*schema.Message, error) {
	if state == nil || state.Plan == nil || len(state.Plan.Steps) == 0 {
		return finalMessageFromGraphState(state), nil
	}
	var completed []string
	var failed []string
	for _, step := range state.Plan.Steps {
		summary := latestEvidenceSummary(step.Evidence)
		switch step.Status {
		case model.PlanStepCompleted:
			line := step.Action
			if summary != "" {
				line = summary
			}
			completed = append(completed, line)
		case model.PlanStepFailed:
			line := step.Action
			if reason, ok := failedPlanExecutionEvidenceReason(step.Evidence); ok && strings.TrimSpace(reason) != "" {
				line = fmt.Sprintf("%s: %s", step.Action, reason)
			}
			failed = append(failed, line)
		}
	}
	if len(failed) == 0 && len(completed) == 1 {
		return schema.AssistantMessage(completed[0], nil), nil
	}
	var b strings.Builder
	if len(failed) == 0 {
		b.WriteString("Completed the requested work.")
	} else if len(completed) == 0 {
		b.WriteString("I could not complete the requested work.")
	} else {
		b.WriteString("Completed part of the requested work, but not everything.")
	}
	if len(completed) > 0 {
		b.WriteString("\n\nCompleted:")
		for _, line := range completed {
			b.WriteString("\n- ")
			b.WriteString(strings.TrimSpace(line))
		}
	}
	if len(failed) > 0 {
		b.WriteString("\n\nNot completed:")
		for _, line := range failed {
			b.WriteString("\n- ")
			b.WriteString(strings.TrimSpace(line))
		}
	}
	return schema.AssistantMessage(strings.TrimSpace(b.String()), nil), nil
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
	if err := n.emitStepFailed(ctx, plan, plan.Steps[stepIndex], reason); err != nil {
		return nil, err
	}
	return plan, nil
}

func (n *ExecuteDispatchNode) emitStepStarted(ctx context.Context, plan *model.Plan, step model.PlanStep) error {
	if n.eventStore == nil {
		return nil
	}
	_, err := stream.AppendStreamItem(ctx, n.eventStore, stream.StreamSinkFromContext(ctx), stream.StreamItem{
		RunID:     plan.RunID,
		Kind:      stream.StreamKindStepStarted,
		CreatedAt: plan.UpdatedAt,
		Payload:   stream.PlanStepPayloadToMap(streamStepPayloadFromPlan(plan, step)),
	})
	if err != nil {
		return fmt.Errorf("append step.started event: %w", err)
	}
	return nil
}

func (n *ExecuteDispatchNode) emitStepCompleted(ctx context.Context, plan *model.Plan, step model.PlanStep) error {
	if n.eventStore == nil {
		return nil
	}
	_, err := stream.AppendStreamItem(ctx, n.eventStore, stream.StreamSinkFromContext(ctx), stream.StreamItem{
		RunID:     plan.RunID,
		Kind:      stream.StreamKindStepCompleted,
		CreatedAt: plan.UpdatedAt,
		Payload:   stream.PlanStepPayloadToMap(streamStepPayloadFromPlan(plan, step)),
	})
	if err != nil {
		return fmt.Errorf("append step.completed event: %w", err)
	}
	return nil
}

func (n *ExecuteDispatchNode) emitStepFailed(ctx context.Context, plan *model.Plan, step model.PlanStep, reason string) error {
	if n.eventStore == nil {
		return nil
	}
	payload := stream.PlanStepPayloadToMap(streamStepPayloadFromPlan(plan, step))
	payload["error"] = reason
	_, err := stream.AppendStreamItem(ctx, n.eventStore, stream.StreamSinkFromContext(ctx), stream.StreamItem{
		RunID:     plan.RunID,
		Kind:      stream.StreamKindStepFailed,
		CreatedAt: plan.UpdatedAt,
		Payload:   payload,
	})
	if err != nil {
		return fmt.Errorf("append step.failed event: %w", err)
	}
	return nil
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

func formatExecuteChildTask(plan *model.Plan, step model.PlanStep) string {
	var b strings.Builder
	b.WriteString("Execute exactly one parent plan step. Finish only this step.\n")
	b.WriteString("Your final output must be the user-facing result for this step, not an execution report. Do not add headings such as \"Completion Summary\" unless the user explicitly asked for a report.\n\n")
	fmt.Fprintf(&b, "Step %s: %s\n", step.ID, strings.TrimSpace(step.Action))
	if len(step.RepoTargets) > 0 {
		b.WriteString("\nRepo targets:\n")
		for _, target := range step.RepoTargets {
			line := strings.TrimSpace(target.Path)
			if strings.TrimSpace(target.Symbol) != "" {
				line = fmt.Sprintf("%s#%s", line, strings.TrimSpace(target.Symbol))
			}
			if strings.TrimSpace(target.Reason) != "" {
				line = fmt.Sprintf("%s (%s)", line, strings.TrimSpace(target.Reason))
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if len(step.VerificationIntent) > 0 {
		b.WriteString("\nVerification requirements:\n")
		for _, intent := range step.VerificationIntent {
			line := strings.TrimSpace(intent.Kind)
			if len(intent.Command) > 0 {
				line = fmt.Sprintf("%s via %s", line, strings.Join(intent.Command, " "))
			}
			if strings.TrimSpace(intent.Reason) != "" {
				line = fmt.Sprintf("%s (%s)", line, strings.TrimSpace(intent.Reason))
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	upstream := completedStepContext(plan, step.ID)
	if upstream != "" {
		b.WriteString("\nUpstream completed context:\n")
		b.WriteString(upstream)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func completedStepContext(plan *model.Plan, currentStepID string) string {
	if plan == nil {
		return ""
	}
	lines := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if step.ID == currentStepID || step.Status != model.PlanStepCompleted {
			continue
		}
		summary := latestEvidenceSummary(step.Evidence)
		line := step.Action
		if summary != "" {
			line = fmt.Sprintf("%s: %s", step.Action, summary)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func stepRequiresVerifier(step model.PlanStep) bool {
	for _, intent := range step.VerificationIntent {
		if strings.TrimSpace(intent.Kind) == "verifier" {
			return true
		}
	}
	return false
}

func verifierAcceptanceCriteria(step model.PlanStep) []string {
	criteria := make([]string, 0, len(step.VerificationIntent)+1)
	action := strings.TrimSpace(step.Action)
	if action != "" {
		criteria = append(criteria, fmt.Sprintf("completed plan step %s: %s", strings.TrimSpace(step.ID), action))
	}
	for _, intent := range step.VerificationIntent {
		if strings.TrimSpace(intent.Kind) != "verifier" {
			continue
		}
		if reason := strings.TrimSpace(intent.Reason); reason != "" {
			criteria = append(criteria, reason)
		}
	}
	return trimmedNonEmptyStrings(criteria)
}

func verifierEvidenceRefs(items []model.PlanEvidence) []string {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if ref := strings.TrimSpace(item.ID); ref != "" {
			refs = append(refs, ref)
		}
	}
	return trimmedNonEmptyStrings(refs)
}

func verifierToolResultRefs(items []model.PlanEvidence) []string {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if ref := strings.TrimSpace(item.ToolResultRef); ref != "" {
			refs = append(refs, ref)
		}
	}
	return trimmedNonEmptyStrings(refs)
}

func verifierReadOnlyToolNames() []string {
	return []string{"read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff"}
}

func subagentEvidenceFromChildResult(stepID, parentRunID string, result *orchestration.ChildAgentResult, recordedAt time.Time) model.PlanEvidence {
	if result == nil {
		return failedChildExecutionEvidence(stepID, parentRunID, errors.New("child result is nil"), recordedAt)
	}
	summary := summarizeChildResult(result)
	status := model.EvidenceStatusPassed
	errText := ""
	if strings.TrimSpace(result.Acceptance.Status) != "passed" {
		status = model.EvidenceStatusFailed
		errText = strings.Join(trimmedNonEmptyStrings(result.Acceptance.Reasons), "; ")
		if errText == "" {
			errText = fmt.Sprintf("child run %s acceptance failed", strings.TrimSpace(result.ChildRunID))
		}
	}
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
		if summary != "" {
			return fmt.Sprintf("Completed step %s: %s", strings.TrimSpace(step.ID), summary)
		}
		return fmt.Sprintf("Completed step %s.", strings.TrimSpace(step.ID))
	}
	if reason, ok := failedPlanExecutionEvidenceReason(step.Evidence); ok && strings.TrimSpace(reason) != "" {
		return fmt.Sprintf("Step %s failed: %s", strings.TrimSpace(step.ID), strings.TrimSpace(reason))
	}
	return fmt.Sprintf("Step %s failed.", strings.TrimSpace(step.ID))
}

func failedPlanExecutionEvidenceReason(items []model.PlanEvidence) (string, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		switch item.Kind {
		case model.EvidenceKindSubagent:
			if item.Status != model.EvidenceStatusFailed {
				continue
			}
		case model.EvidenceKindVerifier:
			if item.Status != model.EvidenceStatusFailed && !(item.Status == model.EvidenceStatusRecorded && strings.TrimSpace(item.Error) != "") {
				continue
			}
		default:
			continue
		}
		if strings.TrimSpace(item.Error) != "" {
			return strings.TrimSpace(item.Error), true
		}
		return strings.TrimSpace(item.Summary), true
	}
	return "", false
}
