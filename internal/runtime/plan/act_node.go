package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/providers"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
)

type ActNode struct {
	model      einomodel.BaseChatModel
	tools      orchestration.ToolInvoker
	streamer   orchestration.AssistantStreamer
	store      runtimeapi.PlanStore
	specs      map[string]tooling.ToolSpec
	eagerTools []string
}

const (
	maxDeferredLoadRoundsPerStep = 2
	maxActionRoundsPerStep       = 4
)

func NewActNode(
	model einomodel.BaseChatModel,
	tools orchestration.ToolInvoker,
	streamer orchestration.AssistantStreamer,
	store runtimeapi.PlanStore,
	specs []tooling.ToolSpec,
	eagerToolNames []string,
) *ActNode {
	return &ActNode{
		model:      model,
		tools:      tools,
		streamer:   streamer,
		store:      store,
		specs:      planPolicySpecsByName(specs),
		eagerTools: append([]string(nil), eagerToolNames...),
	}
}

func (n *ActNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
	if state == nil {
		return nil, fmt.Errorf("act node requires graph state")
	}
	if n == nil || n.model == nil {
		return nil, fmt.Errorf("act node requires a chat model")
	}
	if n.tools == nil {
		return nil, fmt.Errorf("act node requires a tool node")
	}
	if n.store == nil {
		return nil, fmt.Errorf("act node requires a plan store")
	}
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, fmt.Errorf("act node requires session_id")
	}
	runID := strings.TrimSpace(runtimeapi.GetRunID(ctx))
	if runID == "" {
		return nil, fmt.Errorf("act node requires run_id")
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
	}

	step := plan.Steps[stepIndex]
	var toolMessages []*schema.Message
	for round := 0; round < maxActionRoundsPerStep; round++ {
		toolInfos := contextplane.LoadedToolInfosFromContext(ctx, n.eagerTools)
		messageID := fmt.Sprintf("act-%s-%d", step.ID, round)
		modelReq := graph.GraphSessionModelCallRequest(messageID, "agent_graph_act", toolInfos)
		session, baseMessages, err := graph.GraphSessionBaseMessages(ctx, state, modelReq)
		if err != nil {
			return nil, fmt.Errorf("act before model call: %w", err)
		}
		assistantResult, err := n.streamer.StreamAssistantMessage(ctx, orchestration.AssistantStreamRequest{
			RunID:     runID,
			MessageID: messageID,
			Model:     n.model,
			Messages:  n.buildModelInput(baseMessages, step),
			ToolInfos: toolInfos,
			CallSite:  providers.CallSiteAct,
		})
		if contextplane.IsContextOverflowError(err) && session != nil {
			baseMessages, err = graph.GraphSessionReactiveBaseMessages(ctx, session, state, modelReq, err)
			if err != nil {
				return nil, fmt.Errorf("act reactive compact: %w", err)
			}
			assistantResult, err = n.streamer.StreamAssistantMessage(ctx, orchestration.AssistantStreamRequest{
				RunID:     runID,
				MessageID: messageID,
				Model:     n.model,
				Messages:  n.buildModelInput(baseMessages, step),
				ToolInfos: toolInfos,
				CallSite:  providers.CallSiteAct,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("stream step action: %w", err)
		}
		if assistantResult == nil || assistantResult.Message == nil {
			return nil, fmt.Errorf("stream step action returned nil assistant message")
		}
		assistant := assistantResult.Message
		state.Messages = append(state.Messages, assistant)
		if err := graph.GraphSessionRecordAssistant(ctx, session, assistant); err != nil {
			return nil, err
		}
		switch assistantResult.StopReason {
		case orchestration.AssistantStopReasonMaxOutput:
			plan, err = n.failStep(ctx, plan, stepIndex, "act node model hit output token limit before completing tool calls")
			if err != nil {
				return nil, err
			}
			state.Plan = plan
			state.Phase = graph.PhaseAct
			return state, nil
		case orchestration.AssistantStopReasonEndTurn, orchestration.AssistantStopReasonToolCalls:
		case orchestration.AssistantStopReasonUnknown:
			return nil, fmt.Errorf("act node unsupported assistant finish reason %q", assistantResult.RawReason)
		default:
			return nil, fmt.Errorf("act node unsupported assistant stop reason %q", assistantResult.StopReason)
		}
		if len(assistant.ToolCalls) == 0 {
			plan, err = n.failStep(ctx, plan, stepIndex, "act node model returned no tool calls")
			if err != nil {
				return nil, err
			}
			state.Plan = plan
			state.Phase = graph.PhaseAct
			return state, nil
		}
		if err := n.enforceToolCalls(ctx, assistant.ToolCalls); err != nil {
			return nil, err
		}
		toolCallArguments := toolCallArgumentsByID(assistant.ToolCalls)

		toolMessages, err = n.streamToolMessages(ctx, assistant)
		if err != nil {
			if tool.IsInterruptError(err) {
				return nil, err
			}
			plan, failErr := n.failStep(ctx, plan, stepIndex, err.Error())
			if failErr != nil {
				return nil, failErr
			}
			state.Plan = plan
			state.Phase = graph.PhaseAct
			return state, nil
		}
		state.Messages = append(state.Messages, toolMessages...)
		if err := graph.GraphSessionRecordToolResults(ctx, session, toolMessages); err != nil {
			return nil, err
		}
		deferredDefinitions := deferredDefinitionMessages(toolMessages)
		state.Messages = append(state.Messages, deferredDefinitions...)
		if err := graph.GraphSessionRecordMessages(ctx, session, deferredDefinitions); err != nil {
			return nil, err
		}

		for _, toolMessage := range toolMessages {
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
				return nil, fmt.Errorf("record plan step evidence: %w", err)
			}
			for _, evidence := range evidenceItems {
				if _, err := n.store.AppendStepEvidence(ctx, sessionID, runID, step.ID, evidence); err != nil {
					return nil, fmt.Errorf("record plan step evidence: %w", err)
				}
				if strings.TrimSpace(evidence.ToolResultRef) != "" {
					if err := n.store.AppendToolResultEvidenceRef(ctx, evidence.ToolResultRef, store.EvidenceRef{
						Kind: string(evidence.Kind),
						Ref:  evidence.ID,
					}); err != nil {
						return nil, fmt.Errorf("record tool result evidence ref: %w", err)
					}
				}
			}
		}

		plan, stepIndex, err = n.reloadStep(ctx, sessionID, step.ID)
		if err != nil {
			return nil, err
		}
		currentStep := plan.Steps[stepIndex]
		if reason, ok := failedSubagentEvidenceReason(currentStep.Evidence); ok {
			plan, err = n.failStep(ctx, plan, stepIndex, reason)
			if err != nil {
				return nil, err
			}
			state.Plan = plan
			state.Phase = graph.PhaseAct
			return state, nil
		}
		if reason, ok := failedToolMessageReason(toolMessages); ok {
			plan, err = n.failStep(ctx, plan, stepIndex, reason)
			if err != nil {
				return nil, err
			}
			state.Plan = plan
			state.Phase = graph.PhaseAct
			return state, nil
		}

		if containsOnlyLoadToolsCalls(assistant.ToolCalls) {
			if round >= maxDeferredLoadRoundsPerStep {
				plan, err = n.failStep(ctx, plan, stepIndex, "load_tools exhausted without actionable tool call")
				if err != nil {
					return nil, err
				}
				state.Plan = plan
				state.Phase = graph.PhaseAct
				return state, nil
			}
			step = currentStep
			continue
		}

		if err := ensureVerificationIntentCoverage(currentStep); err != nil {
			if round == maxActionRoundsPerStep-1 {
				plan, failErr := n.failStep(ctx, plan, stepIndex, err.Error())
				if failErr != nil {
					return nil, failErr
				}
				state.Plan = plan
				state.Phase = graph.PhaseAct
				return state, nil
			}
			continuation := schema.UserMessage(formatVerificationContinuationPrompt(currentStep, err))
			state.Messages = append(state.Messages, continuation)
			if err := graph.GraphSessionRecordMessages(ctx, session, []*schema.Message{continuation}); err != nil {
				return nil, err
			}
			step = currentStep
			continue
		}

		plan.Steps[stepIndex].Status = model.PlanStepCompleted
		plan.RunID = runID
		plan.UpdatedAt = time.Now().UTC()
		if err := n.store.SavePlan(ctx, plan); err != nil {
			return nil, fmt.Errorf("mark plan step completed: %w", err)
		}

		state.Plan = plan
		state.Phase = graph.PhaseAct
		return state, nil
	}

	plan, stepIndex, err = n.reloadStep(ctx, sessionID, step.ID)
	if err != nil {
		return nil, err
	}
	plan, err = n.failStep(ctx, plan, stepIndex, "act node exhausted action rounds before completing the step")
	if err != nil {
		return nil, err
	}
	state.Plan = plan
	state.Phase = graph.PhaseAct
	return state, nil
}

func formatVerificationContinuationPrompt(step model.PlanStep, coverageErr error) string {
	var b strings.Builder
	b.WriteString("The active plan step is not complete yet. Continue the same step and call the missing tool(s) needed to satisfy verification before finalizing.")
	if coverageErr != nil && strings.TrimSpace(coverageErr.Error()) != "" {
		b.WriteString("\n\nMissing verification: ")
		b.WriteString(strings.TrimSpace(coverageErr.Error()))
	}
	if len(step.Evidence) > 0 {
		b.WriteString("\n\nRecorded evidence so far:")
		for _, evidence := range step.Evidence {
			summary := strings.TrimSpace(evidence.Summary)
			if summary == "" {
				summary = strings.TrimSpace(string(evidence.Kind))
			}
			if summary == "" {
				continue
			}
			b.WriteString("\n- ")
			b.WriteString(summary)
		}
	}
	return b.String()
}

func (n *ActNode) streamToolMessages(ctx context.Context, assistant *schema.Message) ([]*schema.Message, error) {
	executor := n.tools.NewStreamingExecutor(ctx)
	for _, call := range assistant.ToolCalls {
		executor.Submit(call)
	}
	results, err := executor.GetRemainingResults(ctx)
	if err != nil {
		executor.Discard()
		return nil, err
	}
	return results, nil
}

func deferredDefinitionMessages(toolMessages []*schema.Message) []*schema.Message {
	if len(toolMessages) == 0 {
		return nil
	}
	var out []*schema.Message
	for _, toolMessage := range toolMessages {
		if toolMessage == nil || toolMessage.ToolName != "load_tools" {
			continue
		}
		payload := parseLoadToolsOutput(toolMessage.Content)
		if len(payload.Messages) == 0 {
			continue
		}
		for _, content := range payload.Messages {
			trimmed := strings.TrimSpace(content)
			if trimmed == "" {
				continue
			}
			out = append(out, schema.UserMessage(trimmed))
		}
	}
	return out
}

type loadToolsOutputPayload struct {
	Messages []string `json:"messages,omitempty"`
}

func parseLoadToolsOutput(content string) loadToolsOutputPayload {
	var payload loadToolsOutputPayload
	if strings.TrimSpace(content) == "" {
		return payload
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return loadToolsOutputPayload{}
	}
	return payload
}

func (n *ActNode) loadRunnablePlan(ctx context.Context, sessionID string) (*model.Plan, int, error) {
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

func (n *ActNode) buildModelInput(messages []*schema.Message, step model.PlanStep) []*schema.Message {
	instruction := fmt.Sprintf(
		"Execute exactly one active plan step. Return the tool calls needed for this step and do not complete unrelated work.\n\nStep %s: %s",
		step.ID,
		step.Action,
	)
	out := make([]*schema.Message, 0, len(messages)+1)
	out = append(out, messages...)
	out = append(out, schema.UserMessage(instruction))
	return out
}

func (n *ActNode) enforceToolCalls(ctx context.Context, calls []schema.ToolCall) error {
	for _, call := range calls {
		spec, ok := n.specs[strings.TrimSpace(call.Function.Name)]
		if !ok {
			continue
		}
		if _, _, err := enforceRiskyToolPlan(ctx, n.store, spec); err != nil {
			return err
		}
	}
	return nil
}

func (n *ActNode) failStep(ctx context.Context, plan *model.Plan, stepIndex int, reason string) (*model.Plan, error) {
	plan.Steps[stepIndex].Status = model.PlanStepFailed
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("mark plan step failed: %w", err)
	}
	return plan, nil
}

func failedSubagentEvidenceReason(items []model.PlanEvidence) (string, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Kind != model.EvidenceKindSubagent || item.Status != model.EvidenceStatusFailed {
			continue
		}
		if strings.TrimSpace(item.Error) != "" {
			return strings.TrimSpace(item.Error), true
		}
		return strings.TrimSpace(item.Summary), true
	}
	return "", false
}

func (n *ActNode) reloadStep(ctx context.Context, sessionID string, stepID string) (*model.Plan, int, error) {
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

func planPolicySpecsByName(specs []tooling.ToolSpec) map[string]tooling.ToolSpec {
	out := make(map[string]tooling.ToolSpec, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		out[strings.TrimSpace(spec.Name)] = spec
	}
	return out
}

func toolCallArgumentsByID(calls []schema.ToolCall) map[string]string {
	out := make(map[string]string, len(calls))
	for _, call := range calls {
		out[call.ID] = call.Function.Arguments
	}
	return out
}

func failedToolMessageReason(messages []*schema.Message) (string, bool) {
	for _, msg := range messages {
		if msg == nil || msg.Extra == nil {
			continue
		}
		failed, ok := msg.Extra["tool_error"].(bool)
		if !ok || !failed {
			continue
		}
		reason, reasonOK := msg.Extra["tool_error_reason"].(string)
		if !reasonOK {
			reason = ""
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = strings.TrimSpace(msg.Content)
		}
		if reason == "" {
			reason = "tool returned an error result"
		}
		return reason, true
	}
	return "", false
}

func containsOnlyLoadToolsCalls(calls []schema.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !tool.IsLoadToolsCall(call) {
			return false
		}
	}
	return true
}
