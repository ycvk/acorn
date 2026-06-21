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
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/tool"
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

func (n *ActNode) actCompact(session contextplane.ContextSession, state *graph.AgentGraphState, modelReq contextplane.ModelCallRequest, step model.PlanStep) orchestration.CompactFn {
	if session == nil {
		return nil
	}
	return func(ctx context.Context, streamErr error) ([]*schema.Message, error) {
		recovered, err := graph.GraphSessionReactiveBaseMessages(ctx, session, state, modelReq, streamErr)
		if err != nil {
			return nil, fmt.Errorf("act reactive compact: %w", err)
		}
		return n.buildModelInput(recovered, step), nil
	}
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

func (n *ActNode) enforceToolCall(ctx context.Context, call schema.ToolCall) error {
	spec, ok := n.specs[strings.TrimSpace(call.Function.Name)]
	if !ok {
		return nil
	}
	_, _, err := enforceRiskyToolPlan(ctx, n.store, spec)
	return err
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
