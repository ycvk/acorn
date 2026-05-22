package runtime

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
)

type ObserveDecisionType string

const (
	ObserveDecisionNext   ObserveDecisionType = "next"
	ObserveDecisionReplan ObserveDecisionType = "replan"
	ObserveDecisionDone   ObserveDecisionType = "done"
)

type ObserveDecision struct {
	Decision ObserveDecisionType `json:"decision"`
	StepID   string              `json:"step_id,omitempty"`
	Reason   string              `json:"reason,omitempty"`
}

type ObserveNode struct {
	model einomodel.BaseChatModel
	store PlanStore
}

func NewObserveNode(model einomodel.BaseChatModel, store PlanStore) *ObserveNode {
	return &ObserveNode{model: model, store: store}
}

func (n *ObserveNode) Decide(ctx context.Context, state *AgentGraphState) (ObserveDecision, error) {
	if state == nil {
		return ObserveDecision{}, fmt.Errorf("observe node requires graph state")
	}
	if n == nil || n.store == nil {
		return ObserveDecision{}, fmt.Errorf("observe node requires a plan store")
	}
	sessionID := strings.TrimSpace(sessionIDFromContext(ctx))
	if sessionID == "" {
		return ObserveDecision{}, fmt.Errorf("observe node requires session_id")
	}
	plan, err := n.store.LoadPlan(ctx, sessionID)
	if err != nil {
		return ObserveDecision{}, fmt.Errorf("load active plan: %w", err)
	}
	state.Plan = plan
	if allPlanStepsTerminal(plan) {
		return ObserveDecision{Decision: ObserveDecisionDone, Reason: "all plan steps are terminal"}, nil
	}
	if n.model == nil {
		return ObserveDecision{}, fmt.Errorf("observe node requires a chat model")
	}
	modelReq := graphSessionModelCallRequest(graphModelCallID(ctx, "observe"), "agent_graph_observe", nil)
	session, baseMessages, err := graphSessionBaseMessages(ctx, state, modelReq)
	if err != nil {
		return ObserveDecision{}, fmt.Errorf("observe before model call: %w", err)
	}
	msg, err := n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSiteObserve), n.buildModelInput(baseMessages, plan))
	if contextplane.IsContextOverflowError(err) && session != nil {
		baseMessages, err = graphSessionReactiveBaseMessages(ctx, session, state, modelReq, err)
		if err != nil {
			return ObserveDecision{}, fmt.Errorf("observe reactive compact: %w", err)
		}
		msg, err = n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSiteObserve), n.buildModelInput(baseMessages, plan))
	}
	if err != nil {
		return ObserveDecision{}, fmt.Errorf("generate observe decision: %w", err)
	}
	state.Messages = append(state.Messages, msg)
	if err := graphSessionRecordAssistant(ctx, session, msg); err != nil {
		return ObserveDecision{}, err
	}
	decision, err := parseObserveDecision(msg.Content)
	if err != nil {
		return ObserveDecision{}, err
	}
	return decision, nil
}

func (n *ObserveNode) buildModelInput(messages []*schema.Message, plan *Plan) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages)+1)
	out = append(out, messages...)
	out = append(out, schema.UserMessage(formatObservePrompt(plan)))
	return out
}

func parseObserveDecision(content string) (ObserveDecision, error) {
	var decision ObserveDecision
	if err := json.Unmarshal(bytes.TrimSpace([]byte(content)), &decision); err != nil {
		return ObserveDecision{}, fmt.Errorf("parse observe decision JSON: %w", err)
	}
	switch decision.Decision {
	case ObserveDecisionNext:
		if strings.TrimSpace(decision.StepID) == "" {
			return ObserveDecision{}, fmt.Errorf("observe decision next requires step_id")
		}
	case ObserveDecisionReplan, ObserveDecisionDone:
	default:
		return ObserveDecision{}, fmt.Errorf("unknown observe decision %q", decision.Decision)
	}
	decision.StepID = strings.TrimSpace(decision.StepID)
	decision.Reason = strings.TrimSpace(decision.Reason)
	return decision, nil
}

func formatObservePrompt(plan *Plan) string {
	var b strings.Builder
	b.WriteString("Review the current execution plan and decide the next graph transition. Return JSON only: {\"decision\":\"next|replan|done\",\"step_id\":\"sN\",\"reason\":\"...\"}.\n\n")
	for _, step := range plan.Steps {
		fmt.Fprintf(&b, "- %s [%s]: %s\n", step.ID, step.Status, step.Action)
	}
	return b.String()
}
