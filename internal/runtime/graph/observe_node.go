package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/runtime/api"
)

type ObserveNode struct {
	model einomodel.BaseChatModel
	store api.PlanStore
}

func NewObserveNode(model einomodel.BaseChatModel, store api.PlanStore) *ObserveNode {
	return &ObserveNode{model: model, store: store}
}

func (n *ObserveNode) Decide(ctx context.Context, state *AgentGraphState) (ObserveDecision, error) {
	if state == nil {
		return ObserveDecision{}, fmt.Errorf("observe node requires graph state")
	}
	if n == nil || n.store == nil {
		return ObserveDecision{}, fmt.Errorf("observe node requires a plan store")
	}
	sessionID := strings.TrimSpace(api.SessionIDFromContext(ctx))
	if sessionID == "" {
		return ObserveDecision{}, fmt.Errorf("observe node requires session_id")
	}
	plan, err := n.store.LoadPlan(ctx, sessionID)
	if err != nil {
		return ObserveDecision{}, fmt.Errorf("load active plan: %w", err)
	}
	state.Plan = plan
	if AllPlanStepsTerminal(plan) {
		return ObserveDecision{Decision: ObserveDecisionDone, Reason: "all plan steps are terminal"}, nil
	}
	if n.model == nil {
		return ObserveDecision{}, fmt.Errorf("observe node requires a chat model")
	}
	modelReq := GraphSessionModelCallRequest(GraphModelCallID(ctx, "observe"), "agent_graph_observe", nil)
	session, baseMessages, err := GraphSessionBaseMessages(ctx, state, modelReq)
	if err != nil {
		return ObserveDecision{}, fmt.Errorf("observe before model call: %w", err)
	}
	msg, err := n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSiteObserve), n.buildModelInput(baseMessages, plan))
	if contextplane.IsContextOverflowError(err) && session != nil {
		baseMessages, err = GraphSessionReactiveBaseMessages(ctx, session, state, modelReq, err)
		if err != nil {
			return ObserveDecision{}, fmt.Errorf("observe reactive compact: %w", err)
		}
		msg, err = n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSiteObserve), n.buildModelInput(baseMessages, plan))
	}
	if err != nil {
		return ObserveDecision{}, fmt.Errorf("generate observe decision: %w", err)
	}
	state.Messages = append(state.Messages, msg)
	if err := GraphSessionRecordAssistant(ctx, session, msg); err != nil {
		return ObserveDecision{}, err
	}
	decision, err := ParseObserveDecision(msg.Content)
	if err != nil {
		return ObserveDecision{}, err
	}
	return decision, nil
}

func (n *ObserveNode) buildModelInput(messages []*schema.Message, plan *api.Plan) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages)+1)
	out = append(out, messages...)
	out = append(out, schema.UserMessage(formatObservePrompt(plan)))
	return out
}

func ParseObserveDecision(content string) (ObserveDecision, error) {
	var decision ObserveDecision
	if err := json.Unmarshal(bytes.TrimSpace([]byte(content)), &decision); err != nil {
		return ObserveDecision{}, fmt.Errorf("parse observe decision JSON: %w", err)
	}
	switch decision.Decision {
	case ObserveDecisionNext:
		if strings.TrimSpace(decision.StepID) == "" {
			return ObserveDecision{}, fmt.Errorf("observe decision 'next' requires step_id")
		}
	case ObserveDecisionReplan, ObserveDecisionDone:
	default:
		return ObserveDecision{}, fmt.Errorf("unknown observe decision: %s", decision.Decision)
	}
	decision.StepID = strings.TrimSpace(decision.StepID)
	decision.Reason = strings.TrimSpace(decision.Reason)
	return decision, nil
}

func formatObservePrompt(plan *api.Plan) string {
	if plan == nil {
		return "No active plan."
	}
	return fmt.Sprintf("Review the plan and decide next action:\n%s", FormatPlanSummary(plan))
}
