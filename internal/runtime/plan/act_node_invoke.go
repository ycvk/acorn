package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/providers"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/store"
)

func (n *ActNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
	if err := n.validateActState(state); err != nil {
		return nil, err
	}
	sessionID, runID, err := actSessionIDs(ctx)
	if err != nil {
		return nil, err
	}
	plan, stepIndex, step, err := n.loadActStep(ctx, sessionID, runID)
	if err != nil {
		return nil, err
	}
	for round := 0; round < maxActionRoundsPerStep; round++ {
		outcome, nextStep, err := n.runActRound(ctx, state, plan, stepIndex, step, runID, sessionID, round)
		if err != nil {
			return nil, err
		}
		if outcome.done {
			return state, nil
		}
		step = nextStep
		plan = outcome.plan
		stepIndex = outcome.stepIndex
	}
	return n.failExhaustedActRounds(ctx, state, sessionID, step)
}

func actSessionIDs(ctx context.Context) (string, string, error) {
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	runID := strings.TrimSpace(runtimeapi.GetRunID(ctx))
	return sessionID, runID, validateActIDs(sessionID, runID)
}

func (n *ActNode) validateActState(state *graph.AgentGraphState) error {
	if state == nil {
		return fmt.Errorf("act node requires graph state")
	}
	if n == nil || n.model == nil {
		return fmt.Errorf("act node requires a chat model")
	}
	if n.tools == nil {
		return fmt.Errorf("act node requires a tool node")
	}
	if n.streamer == nil {
		return fmt.Errorf("act node requires assistant streamer")
	}
	if n.store == nil {
		return fmt.Errorf("act node requires a plan store")
	}
	return nil
}

func validateActIDs(sessionID, runID string) error {
	if sessionID == "" {
		return fmt.Errorf("act node requires session_id")
	}
	if runID == "" {
		return fmt.Errorf("act node requires run_id")
	}
	return nil
}

type actRoundOutcome struct {
	done      bool
	plan      *model.Plan
	stepIndex int
}

func (n *ActNode) markActStepStarted(ctx context.Context, plan *model.Plan, stepIndex int, runID string) (*model.Plan, int, error) {
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

func (n *ActNode) loadActStep(ctx context.Context, sessionID, runID string) (*model.Plan, int, model.PlanStep, error) {
	plan, stepIndex, err := n.loadRunnablePlan(ctx, sessionID)
	if err != nil {
		return nil, 0, model.PlanStep{}, err
	}
	plan, stepIndex, err = n.markActStepStarted(ctx, plan, stepIndex, runID)
	if err != nil {
		return nil, 0, model.PlanStep{}, err
	}
	return plan, stepIndex, plan.Steps[stepIndex], nil
}

func (n *ActNode) runActRound(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, step model.PlanStep, runID, sessionID string, round int) (actRoundOutcome, model.PlanStep, error) {
	assistant, toolMessages, session, outputLimitReached, roundErr := n.executeActRound(ctx, state, plan, step, runID, round)
	// Record assistant message before handling errors — matches original Invoke
	// ordering so ToolExecutionError/interrupt paths still persist the assistant turn.
	if err := n.recordActAssistant(ctx, state, session, assistant); err != nil {
		return actRoundOutcome{}, step, err
	}
	if done, err := n.handleActRoundError(ctx, state, plan, stepIndex, roundErr); done || err != nil {
		return actRoundOutcome{done: true}, step, err
	}
	if done, err := n.handleActRoundResult(ctx, state, plan, stepIndex, assistant, outputLimitReached); done || err != nil {
		return actRoundOutcome{done: true}, step, err
	}
	if err := n.recordActToolResults(ctx, state, session, step, runID, sessionID, assistant, toolMessages); err != nil {
		return actRoundOutcome{}, step, err
	}
	return n.processActRoundOutcome(ctx, state, plan, stepIndex, step, assistant, toolMessages, round)
}

func (n *ActNode) executeActRound(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, step model.PlanStep, runID string, round int) (*schema.Message, []*schema.Message, contextplane.ContextSession, bool, error) {
	toolInfos := contextplane.LoadedToolInfosFromContext(ctx, n.eagerTools)
	messageID := fmt.Sprintf("act-%s-%d", step.ID, round)
	modelReq := graph.GraphSessionModelCallRequest(messageID, "agent_graph_act", toolInfos)
	session, baseMessages, err := graph.GraphSessionBaseMessages(ctx, state, modelReq)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("act before model call: %w", err)
	}
	assistant, toolMessages, outputLimitReached, roundErr := orchestration.RunActionRound(
		ctx, n.model, n.streamer, n.tools, n.buildModelInput(baseMessages, step),
		toolInfos, runID, messageID, true, n.actCompact(session, state, modelReq, step),
		n.actRoundOptions(),
	)
	return assistant, toolMessages, session, outputLimitReached, roundErr
}
func (n *ActNode) actRoundOptions() orchestration.RoundOptions {
	return orchestration.RoundOptions{
		CallSite: providers.CallSiteAct,
		BeforeToolCall: func(ctx context.Context, call schema.ToolCall) error {
			return n.enforceToolCall(ctx, call)
		},
	}
}

func (n *ActNode) handleActRoundError(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, roundErr error) (bool, error) {
	if roundErr == nil {
		return false, nil
	}
	var toolExecErr *orchestration.ToolExecutionError
	if !errors.As(roundErr, &toolExecErr) {
		return true, fmt.Errorf("stream step action: %w", roundErr)
	}
	if tool.IsInterruptError(roundErr) {
		return true, roundErr
	}
	failedPlan, failErr := n.failStep(ctx, plan, stepIndex, roundErr.Error())
	if failErr != nil {
		return true, failErr
	}
	state.Plan = failedPlan
	state.Phase = graph.PhaseAct
	return true, nil
}

func (n *ActNode) handleActRoundResult(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, assistant *schema.Message, outputLimitReached bool) (bool, error) {
	if assistant == nil {
		return true, fmt.Errorf("stream step action returned nil assistant message")
	}
	if outputLimitReached {
		return n.failActStep(ctx, state, plan, stepIndex, "act node model hit output token limit before completing tool calls")
	}
	if len(assistant.ToolCalls) == 0 {
		return n.failActStep(ctx, state, plan, stepIndex, "act node model returned no tool calls")
	}
	return false, nil
}

func (n *ActNode) failActStep(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, reason string) (bool, error) {
	failedPlan, err := n.failStep(ctx, plan, stepIndex, reason)
	if err != nil {
		return true, err
	}
	state.Plan = failedPlan
	state.Phase = graph.PhaseAct
	return true, nil
}

func (n *ActNode) recordActAssistant(ctx context.Context, state *graph.AgentGraphState, session contextplane.ContextSession, assistant *schema.Message) error {
	if assistant == nil {
		return nil
	}
	state.Messages = append(state.Messages, assistant)
	return graph.GraphSessionRecordAssistant(ctx, session, assistant)
}

func (n *ActNode) recordActToolResults(ctx context.Context, state *graph.AgentGraphState, session contextplane.ContextSession, step model.PlanStep, runID, sessionID string, assistant *schema.Message, toolMessages []*schema.Message) error {
	toolCallArguments := toolCallArgumentsByID(assistant.ToolCalls)
	state.Messages = append(state.Messages, toolMessages...)
	deferredDefinitions := deferredDefinitionMessages(toolMessages)
	state.Messages = append(state.Messages, deferredDefinitions...)
	if err := graph.GraphSessionRecordToolResults(ctx, session, toolMessages); err != nil {
		return err
	}
	if err := graph.GraphSessionRecordMessages(ctx, session, deferredDefinitions); err != nil {
		return err
	}
	return n.recordToolMessagesEvidence(ctx, sessionID, runID, step, toolMessages, toolCallArguments)
}

func (n *ActNode) recordToolMessagesEvidence(ctx context.Context, sessionID, runID string, step model.PlanStep, toolMessages []*schema.Message, toolCallArguments map[string]string) error {
	for _, toolMessage := range toolMessages {
		if err := n.recordSingleToolEvidence(ctx, sessionID, runID, step, toolMessage, toolCallArguments); err != nil {
			return err
		}
	}
	return nil
}

func (n *ActNode) recordSingleToolEvidence(ctx context.Context, sessionID, runID string, step model.PlanStep, toolMessage *schema.Message, toolCallArguments map[string]string) error {
	argumentsJSON := toolCallArguments[toolMessage.ToolCallID]
	recordedAt := time.Now().UTC()
	message := &planToolMessage{
		Content: toolMessage.Content,
		Extra:   toolMessage.Extra,
	}
	evidenceItems, err := evidenceForToolMessage(toolMessageEvidenceInput{
		Step:          step,
		RunID:         runID,
		ToolName:      toolMessage.ToolName,
		ToolCallID:    toolMessage.ToolCallID,
		ArgumentsJSON: argumentsJSON,
		Message:       message,
		RecordedAt:    recordedAt,
	})
	if err != nil {
		return fmt.Errorf("record plan step evidence: %w", err)
	}
	return n.appendEvidenceItems(ctx, sessionID, runID, step.ID, evidenceItems)
}

func (n *ActNode) appendEvidenceItems(ctx context.Context, sessionID, runID, stepID string, evidenceItems []model.PlanEvidence) error {
	for _, evidence := range evidenceItems {
		if _, err := n.store.AppendStepEvidence(ctx, sessionID, runID, stepID, evidence); err != nil {
			return fmt.Errorf("record plan step evidence: %w", err)
		}
		if strings.TrimSpace(evidence.ToolResultRef) == "" {
			continue
		}
		if err := n.store.AppendToolResultEvidenceRef(ctx, evidence.ToolResultRef, store.EvidenceRef{
			Kind: string(evidence.Kind),
			Ref:  evidence.ID,
		}); err != nil {
			return fmt.Errorf("record tool result evidence ref: %w", err)
		}
	}
	return nil
}

func (n *ActNode) processActRoundOutcome(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, step model.PlanStep, assistant *schema.Message, toolMessages []*schema.Message, round int) (actRoundOutcome, model.PlanStep, error) {
	reloadedPlan, reloadedIndex, err := n.reloadStep(ctx, strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx)), step.ID)
	if err != nil {
		return actRoundOutcome{}, step, err
	}
	currentStep := reloadedPlan.Steps[reloadedIndex]
	if done, err := n.checkActStepFailures(ctx, state, reloadedPlan, reloadedIndex, currentStep, toolMessages); done || err != nil {
		return actRoundOutcome{done: true}, currentStep, err
	}
	return n.handleActCompletion(ctx, state, reloadedPlan, reloadedIndex, currentStep, assistant, round)
}

func (n *ActNode) checkActStepFailures(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, currentStep model.PlanStep, toolMessages []*schema.Message) (bool, error) {
	if reason, ok := failedSubagentEvidenceReason(currentStep.Evidence); ok {
		return n.failActStep(ctx, state, plan, stepIndex, reason)
	}
	if reason, ok := failedToolMessageReason(toolMessages); ok {
		return n.failActStep(ctx, state, plan, stepIndex, reason)
	}
	return false, nil
}

func (n *ActNode) handleActCompletion(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, currentStep model.PlanStep, assistant *schema.Message, round int) (actRoundOutcome, model.PlanStep, error) {
	if containsOnlyLoadToolsCalls(assistant.ToolCalls) {
		return n.handleLoadToolsDeferral(ctx, state, plan, stepIndex, currentStep, round)
	}
	if err := ensureVerificationIntentCoverage(currentStep); err != nil {
		return n.handleVerificationCoverage(ctx, state, plan, stepIndex, currentStep, round, err)
	}
	return n.markActStepCompleted(ctx, state, plan, stepIndex)
}

func (n *ActNode) handleLoadToolsDeferral(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, currentStep model.PlanStep, round int) (actRoundOutcome, model.PlanStep, error) {
	if round < maxDeferredLoadRoundsPerStep {
		return actRoundOutcome{plan: plan, stepIndex: stepIndex}, currentStep, nil
	}
	failedPlan, err := n.failStep(ctx, plan, stepIndex, "load_tools exhausted without actionable tool call")
	if err != nil {
		return actRoundOutcome{}, currentStep, err
	}
	state.Plan = failedPlan
	state.Phase = graph.PhaseAct
	return actRoundOutcome{done: true}, currentStep, nil
}

func (n *ActNode) handleVerificationCoverage(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int, currentStep model.PlanStep, round int, coverageErr error) (actRoundOutcome, model.PlanStep, error) {
	if round == maxActionRoundsPerStep-1 {
		failedPlan, err := n.failStep(ctx, plan, stepIndex, coverageErr.Error())
		if err != nil {
			return actRoundOutcome{}, currentStep, err
		}
		state.Plan = failedPlan
		state.Phase = graph.PhaseAct
		return actRoundOutcome{done: true}, currentStep, nil
	}
	continuation := schema.UserMessage(formatVerificationContinuationPrompt(currentStep, coverageErr))
	state.Messages = append(state.Messages, continuation)
	if err := graph.GraphSessionRecordMessages(ctx, contextplane.ContextSessionFromContext(ctx), []*schema.Message{continuation}); err != nil {
		return actRoundOutcome{}, currentStep, err
	}
	return actRoundOutcome{plan: plan, stepIndex: stepIndex}, currentStep, nil
}

func (n *ActNode) markActStepCompleted(ctx context.Context, state *graph.AgentGraphState, plan *model.Plan, stepIndex int) (actRoundOutcome, model.PlanStep, error) {
	plan.Steps[stepIndex].Status = model.PlanStepCompleted
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return actRoundOutcome{}, model.PlanStep{}, fmt.Errorf("mark plan step completed: %w", err)
	}
	state.Plan = plan
	state.Phase = graph.PhaseAct
	return actRoundOutcome{done: true}, model.PlanStep{}, nil
}

func (n *ActNode) failExhaustedActRounds(ctx context.Context, state *graph.AgentGraphState, sessionID string, step model.PlanStep) (*graph.AgentGraphState, error) {
	plan, stepIndex, err := n.reloadStep(ctx, sessionID, step.ID)
	if err != nil {
		return nil, err
	}
	failedPlan, err := n.failStep(ctx, plan, stepIndex, "act node exhausted action rounds before completing the step")
	if err != nil {
		return nil, err
	}
	state.Plan = failedPlan
	state.Phase = graph.PhaseAct
	return state, nil
}
