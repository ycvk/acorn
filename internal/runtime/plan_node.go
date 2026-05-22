package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/providerusage"
	storecore "github.com/ycvk/acorn/internal/store"
)

type PlanNode struct {
	model                  einomodel.BaseChatModel
	store                  PlanStore
	eventStore             eventAppender
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
	store PlanStore,
	eventStore eventAppender,
	prompt string,
	planningPromptProvider PlanningPromptProvider,
	enabledToolNames []string,
) *PlanNode {
	return &PlanNode{
		model:                  model,
		store:                  store,
		eventStore:             eventStore,
		prompt:                 strings.TrimSpace(prompt),
		planningPromptProvider: planningPromptProvider,
		enabledToolNames:       append([]string(nil), enabledToolNames...),
	}
}

func (n *PlanNode) Invoke(ctx context.Context, state *AgentGraphState) (*AgentGraphState, error) {
	if state == nil {
		return nil, fmt.Errorf("plan node requires graph state")
	}
	if n == nil || n.model == nil {
		return nil, fmt.Errorf("plan node requires a chat model")
	}
	if n.store == nil {
		return nil, fmt.Errorf("plan node requires a plan store")
	}
	sessionID := strings.TrimSpace(sessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, fmt.Errorf("plan node requires session_id")
	}
	runID := strings.TrimSpace(getRunID(ctx))
	if runID == "" {
		return nil, fmt.Errorf("plan node requires run_id")
	}

	existing, err := n.store.LoadPlan(ctx, sessionID)
	if err != nil && !errors.Is(err, storecore.ErrPlanNotFound) {
		return nil, fmt.Errorf("load existing plan: %w", err)
	}
	if errors.Is(err, storecore.ErrPlanNotFound) {
		existing = nil
	}
	if existingPlanReusable(state, existing) {
		state.Plan = existing
		state.Phase = phasePlan
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
	plan := &Plan{
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
	if n.eventStore != nil {
		if err := n.emitPlanEvent(ctx, plan, existing != nil); err != nil {
			return nil, err
		}
	}

	state.Plan = plan
	state.Phase = phasePlan
	return state, nil
}

func existingPlanReusable(state *AgentGraphState, plan *Plan) bool {
	if plan == nil {
		return false
	}
	if state != nil && state.ObserveDecision.Decision == ObserveDecisionReplan {
		return false
	}
	_, err := findRunnablePlanStep(plan)
	return err == nil
}

func (n *PlanNode) generatePlanSteps(ctx context.Context, state *AgentGraphState) ([]PlanStep, error) {
	modelReq := graphSessionModelCallRequest(graphModelCallID(ctx, "plan"), "agent_graph_plan", nil)
	session, baseMessages, err := graphSessionBaseMessages(ctx, state, modelReq)
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
		msg, err := n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSitePlan), input)
		if contextplane.IsContextOverflowError(err) && session != nil {
			baseMessages, err = graphSessionReactiveBaseMessages(ctx, session, state, modelReq, err)
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
			msg, err = n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSitePlan), input)
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

func (n *PlanNode) emitPlanEvent(ctx context.Context, plan *Plan, update bool) error {
	payload := StreamPayload(&PlanCreatedPayload{Plan: streamPlanFromDomain(plan)})
	kind := StreamKindPlanCreated
	if update {
		kind = StreamKindPlanUpdated
		payload = &PlanUpdatedPayload{Plan: streamPlanFromDomain(plan)}
	}
	if _, err := appendStreamItem(ctx, n.eventStore, streamSinkFromContext(ctx), StreamItem{
		RunID:     plan.RunID,
		Kind:      kind,
		CreatedAt: plan.UpdatedAt,
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("append plan event: %w", err)
	}
	return nil
}

func parsePlanSteps(content string) ([]PlanStep, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, fmt.Errorf("empty plan response")
	}
	var envelope struct {
		Steps []PlanStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil && envelope.Steps != nil {
		return envelope.Steps, nil
	}
	var steps []PlanStep
	if err := json.Unmarshal([]byte(trimmed), &steps); err != nil {
		return nil, fmt.Errorf("parse plan JSON: %w", err)
	}
	return steps, nil
}

func validatePlanSteps(steps []PlanStep, enabledToolNames []string) error {
	if len(steps) == 0 {
		return fmt.Errorf("plan must contain at least one step")
	}
	ids := make(map[string]bool, len(steps))
	for i, step := range steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("step %d id is required", i)
		}
		if ids[id] {
			return fmt.Errorf("duplicate step id %q", id)
		}
		ids[id] = true
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("step %s action is required", id)
		}
		if step.Status != "" && step.Status != PlanStepPending {
			return fmt.Errorf("step %s initial status must be pending", id)
		}
		if err := validatePlanStepMetadata(step, enabledToolNames); err != nil {
			return err
		}
	}
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			depID := strings.TrimSpace(dep)
			if depID == "" {
				return fmt.Errorf("step %s has empty dependency", step.ID)
			}
			if !ids[depID] {
				return fmt.Errorf("step %s depends on unknown step %s", step.ID, depID)
			}
			if depID == strings.TrimSpace(step.ID) {
				return fmt.Errorf("step %s depends on itself", step.ID)
			}
		}
	}
	if err := detectPlanStepCycle(steps); err != nil {
		return err
	}
	return nil
}

func validatePlanStepMetadata(step PlanStep, enabledToolNames []string) error {
	stepID := strings.TrimSpace(step.ID)
	for i, target := range step.RepoTargets {
		path := strings.TrimSpace(target.Path)
		if path == "" {
			return fmt.Errorf("step %s repo_targets[%d].path is required", stepID, i)
		}
		if strings.HasPrefix(path, "/") || containsParentPathSegment(path) {
			return fmt.Errorf("step %s repo_targets[%d].path must be workspace-relative: %s", stepID, i, path)
		}
		confidence := strings.TrimSpace(target.Confidence)
		if confidence != "high" && confidence != "medium" && confidence != "low" {
			return fmt.Errorf("step %s repo_targets[%d].confidence must be high, medium, or low", stepID, i)
		}
		if confidence == "low" && strings.TrimSpace(target.Reason) == "" {
			return fmt.Errorf("step %s repo_targets[%d].reason is required for low confidence", stepID, i)
		}
	}
	switch step.Risk {
	case PlanStepRiskRead, PlanStepRiskWrite, PlanStepRiskExecute, PlanStepRiskDelegate:
	default:
		return fmt.Errorf("step %s risk must be read, write, execute, or delegate", stepID)
	}
	for i, intent := range step.VerificationIntent {
		kind := strings.TrimSpace(intent.Kind)
		if !validVerificationIntentKind(kind) {
			return fmt.Errorf("step %s verification_intent[%d].kind is invalid: %s", stepID, i, kind)
		}
	}
	if step.Risk == PlanStepRiskWrite || step.Risk == PlanStepRiskExecute || step.Risk == PlanStepRiskDelegate {
		if len(step.VerificationIntent) == 0 {
			return fmt.Errorf("step %s risk %s requires verification_intent", stepID, step.Risk)
		}
	}
	enabledTools := map[string]bool{}
	for _, name := range enabledToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		enabledTools[trimmed] = true
	}
	for _, hint := range step.ToolHints {
		name := strings.TrimSpace(hint)
		if name == "" {
			return fmt.Errorf("step %s tool_hints contains an empty tool name", stepID)
		}
		if !enabledTools[name] {
			return fmt.Errorf("step %s tool_hints contains unknown tool %q", stepID, name)
		}
	}
	return nil
}

func containsParentPathSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func validVerificationIntentKind(kind string) bool {
	switch kind {
	case "test", "build", "lint", "diff", "read", "manual", "subagent", "verifier", "checkpoint", "rollback":
		return true
	default:
		return false
	}
}

func detectPlanStepCycle(steps []PlanStep) error {
	deps := make(map[string][]string, len(steps))
	for _, step := range steps {
		id := strings.TrimSpace(step.ID)
		for _, dep := range step.DependsOn {
			deps[id] = append(deps[id], strings.TrimSpace(dep))
		}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("plan dependencies contain a cycle at %s", id)
		}
		visiting[id] = true
		for _, dep := range deps[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for _, step := range steps {
		if err := visit(strings.TrimSpace(step.ID)); err != nil {
			return err
		}
	}
	return nil
}

func normalizePlanSteps(steps []PlanStep) []PlanStep {
	out := make([]PlanStep, 0, len(steps))
	for _, step := range steps {
		normalized := clonePlanStep(step)
		normalized.ID = strings.TrimSpace(normalized.ID)
		normalized.Action = strings.TrimSpace(normalized.Action)
		if normalized.Status == "" {
			normalized.Status = PlanStepPending
		}
		if normalized.Risk == "" {
			normalized.Risk = PlanStepRiskRead
		}
		deps := make([]string, 0, len(normalized.DependsOn))
		for _, dep := range normalized.DependsOn {
			deps = append(deps, strings.TrimSpace(dep))
		}
		normalized.DependsOn = deps
		normalized.RepoTargets = normalizePlanRepoTargets(normalized.RepoTargets)
		normalized.VerificationIntent = normalizeVerificationIntents(normalized.VerificationIntent)
		normalized.ToolHints = normalizeStringList(normalized.ToolHints)
		out = append(out, normalized)
	}
	return out
}

func normalizePlanRepoTargets(items []PlanRepoTarget) []PlanRepoTarget {
	out := make([]PlanRepoTarget, 0, len(items))
	for _, item := range items {
		normalized := item
		normalized.Path = strings.TrimSpace(normalized.Path)
		normalized.Symbol = strings.TrimSpace(normalized.Symbol)
		normalized.Reason = strings.TrimSpace(normalized.Reason)
		normalized.Confidence = strings.TrimSpace(normalized.Confidence)
		out = append(out, normalized)
	}
	return out
}

func normalizeVerificationIntents(items []VerificationIntent) []VerificationIntent {
	out := make([]VerificationIntent, 0, len(items))
	for _, item := range items {
		normalized := item
		normalized.Kind = strings.TrimSpace(normalized.Kind)
		normalized.Reason = strings.TrimSpace(normalized.Reason)
		normalized.Command = normalizeStringList(normalized.Command)
		normalized.Paths = normalizeStringList(normalized.Paths)
		out = append(out, normalized)
	}
	return out
}

func normalizeStringList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.TrimSpace(item))
	}
	return out
}
