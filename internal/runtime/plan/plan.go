package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/providers"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/store"
)

type PlanNode struct {
	model                  einomodel.BaseChatModel
	store                  runtimeapi.PlanStore
	prompt                 string
	planningPromptProvider PlanningPromptProvider
	enabledToolNames       []string
}

type PlanningPromptProvider interface {
	BuildPlanningPromptSection(enabledToolNames []string) (string, error)
}

const planNodeSystemPrompt = `You are Acorn's internal planning node.

Your job is to convert the current user request and conversation context into an executable JSON plan for another agent.
Return JSON only. Do not answer the user directly. Do not include markdown, code fences, comments, or prose outside the JSON object.

The response must be exactly one JSON object with this shape:
{"steps":[{"id":"s1","action":"...","status":"pending","depends_on":[],"risk":"read","repo_targets":[],"verification_intent":[],"tool_hints":[]}]}

Rules:
- Produce at least one step.
- Return a top-level object with a "steps" array. Do not return a single step object.
- Use stable step ids like "s1", "s2".
- Every initial step status must be "pending".
- For greetings or simple conversational requests, create one read-risk step that tells the execution agent to answer the user directly.
- risk must be one of "read", "write", "execute", or "delegate".
- write, execute, and delegate steps must include verification_intent.
- Use verification_intent kind "test" only for an actual test command or test runner. Use "checkpoint" for mutation checkpoint proof and "rollback" for rollback_workspace_checkpoint success proof. Use "verifier" only when the step needs an independent read-only verifier child run after execution evidence exists.
- Do not split tool-result-dependent operations across steps. If a later tool call needs an id or output from an earlier tool call, such as checkpoint_id followed by rollback_workspace_checkpoint, keep those calls in one step.
- repo_targets must be an array of objects like {"path":"README.md","reason":"why","confidence":"high"}. Use [] when no concrete repo target is needed. Never use strings in repo_targets.
- repo_targets paths must be workspace-relative.
- verification_intent must be an array of objects like {"kind":"test","reason":"why"}. Never use strings in verification_intent.
- tool_hints must be an array of tool name strings.`

func NewPlanNode(
	model einomodel.BaseChatModel,
	store runtimeapi.PlanStore,
	prompt string,
	planningPromptProvider PlanningPromptProvider,
	enabledToolNames []string,
) *PlanNode {
	return &PlanNode{
		model:                  model,
		store:                  store,
		prompt:                 strings.TrimSpace(prompt),
		planningPromptProvider: planningPromptProvider,
		enabledToolNames:       append([]string(nil), enabledToolNames...),
	}
}

func (n *PlanNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
	if state == nil {
		return nil, fmt.Errorf("plan node requires graph state")
	}
	if n == nil || n.model == nil {
		return nil, fmt.Errorf("plan node requires a chat model")
	}
	if n.store == nil {
		return nil, fmt.Errorf("plan node requires a plan store")
	}
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, fmt.Errorf("plan node requires session_id")
	}
	runID := strings.TrimSpace(runtimeapi.GetRunID(ctx))
	if runID == "" {
		return nil, fmt.Errorf("plan node requires run_id")
	}

	existing, err := n.store.LoadPlan(ctx, sessionID)
	if err != nil && !errors.Is(err, store.ErrPlanNotFound) {
		return nil, fmt.Errorf("load existing plan: %w", err)
	}
	if errors.Is(err, store.ErrPlanNotFound) {
		existing = nil
	}
	if existingPlanReusable(state, existing) {
		state.Plan = existing
		state.Phase = graph.PhasePlan
		return state, nil
	}

	steps, err := n.generatePlanSteps(ctx, state)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	planID := sessionID
	createdAt := now
	if existing != nil {
		planID = existing.PlanID
		createdAt = existing.CreatedAt
	}
	plan := &model.Plan{
		PlanID:    planID,
		SessionID: sessionID,
		RunID:     runID,
		Steps:     steps,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("save plan: %w", err)
	}

	state.Plan = plan
	state.Phase = graph.PhasePlan
	return state, nil
}

func existingPlanReusable(state *graph.AgentGraphState, plan *model.Plan) bool {
	if plan == nil {
		return false
	}
	if state != nil && state.ObserveDecision.Decision == graph.ObserveDecisionReplan {
		return false
	}
	_, err := graph.FindRunnablePlanStep(plan)
	return err == nil
}

func (n *PlanNode) generatePlanSteps(ctx context.Context, state *graph.AgentGraphState) ([]model.PlanStep, error) {
	modelReq := graph.GraphSessionModelCallRequest(graph.GraphModelCallID(ctx, "plan"), "agent_graph_plan", nil)
	session, baseMessages, err := graph.GraphSessionBaseMessages(ctx, state, modelReq)
	if err != nil {
		return nil, fmt.Errorf("plan before model call: %w", err)
	}
	modelInput, err := n.buildModelInput(baseMessages)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		input := modelInput
		if attempt > 0 {
			input = appendPlanRepairMessage(modelInput, lastErr)
		}
		msg, err := n.model.Generate(providers.WithCallSite(ctx, providers.CallSitePlan), input)
		if contextplane.IsContextOverflowError(err) && session != nil {
			baseMessages, err = graph.GraphSessionReactiveBaseMessages(ctx, session, state, modelReq, err)
			if err != nil {
				return nil, fmt.Errorf("plan reactive compact: %w", err)
			}
			modelInput, err = n.buildModelInput(baseMessages)
			if err != nil {
				return nil, err
			}
			input = modelInput
			if attempt > 0 {
				input = appendPlanRepairMessage(modelInput, lastErr)
			}
			msg, err = n.model.Generate(providers.WithCallSite(ctx, providers.CallSitePlan), input)
		}
		if err != nil {
			return nil, fmt.Errorf("generate plan: %w", err)
		}
		steps, err := parsePlanSteps(msg.Content)
		if err == nil {
			steps = normalizePlanSteps(steps)
			if validateErr := validatePlanSteps(steps, n.enabledToolNames); validateErr != nil {
				err = validateErr
			}
		}
		if err == nil {
			return steps, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("new plan format: %w", lastErr)
}

func (n *PlanNode) buildModelInput(messages []*schema.Message) ([]*schema.Message, error) {
	out := make([]*schema.Message, 0, len(messages)+1)
	promptParts := []string{planNodeSystemPrompt}
	if n.prompt != "" {
		promptParts = append(promptParts, fmt.Sprintf("<agent-instructions>\n%s\n</agent-instructions>\nUse these instructions only as execution constraints when drafting plan steps. They do not override the JSON-only output contract.", n.prompt))
	}
	if n.planningPromptProvider != nil {
		section, err := n.planningPromptProvider.BuildPlanningPromptSection(n.enabledToolNames)
		if err != nil {
			return nil, err
		}
		promptParts = append(promptParts, repoAwarePlanPromptInstruction(section))
	}
	prompt := strings.Join(promptParts, "\n\n")
	if prompt != "" {
		out = append(out, schema.SystemMessage(prompt))
	}
	out = append(out, messages...)
	return out, nil
}

func appendPlanRepairMessage(base []*schema.Message, lastErr error) []*schema.Message {
	out := append([]*schema.Message(nil), base...)
	reason := "the previous response was not a valid plan"
	if lastErr != nil {
		reason = lastErr.Error()
	}
	out = append(out, schema.UserMessage(fmt.Sprintf(
		"The previous planning response was invalid: %s.\nReturn the corrected plan JSON only, with no prose, markdown, or code fence.",
		strings.TrimSpace(reason),
	)))
	return out
}

func repoAwarePlanPromptInstruction(planningContext string) string {
	return fmt.Sprintf(`<planning-context>
%s
</planning-context>

Return a JSON object with a "steps" array. Each step must include:
- id, action, status, depends_on
- repo_targets: workspace-relative paths with reason and confidence
- verification_intent: planned verification actions for write/execute/delegate steps
- risk: read, write, execute, or delegate
- tool_hints: enabled tools likely useful for the step

Use only enabled_tools for tool_hints. Do not treat tool_hints as permission to bypass runtime tool policy.
Use verification_intent kind "test" only for actual test commands. Use "checkpoint" for mutation checkpoint proof and "rollback" for rollback_workspace_checkpoint success proof. Use "verifier" only when an independent read-only verifier child run should review the step evidence.
Do not split tool-result-dependent operations across steps. If a later tool call needs an id or output from an earlier tool call, such as checkpoint_id followed by rollback_workspace_checkpoint, keep those calls in one step.`, planningContext)
}
