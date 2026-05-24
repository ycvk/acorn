package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/stream"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/toolresult"
)

type PlanNode struct {
	model                  einomodel.BaseChatModel
	store                  PlanStore
	eventStore             EventAppender
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
	eventStore EventAppender,
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
	sessionID := strings.TrimSpace(SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, fmt.Errorf("plan node requires session_id")
	}
	runID := strings.TrimSpace(getRunID(ctx))
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
	state.Phase = graph.PhasePlan
	return state, nil
}

func existingPlanReusable(state *graph.AgentGraphState, plan *Plan) bool {
	if plan == nil {
		return false
	}
	if state != nil && state.ObserveDecision.Decision == graph.ObserveDecisionReplan {
		return false
	}
	_, err := graph.FindRunnablePlanStep(plan)
	return err == nil
}

func (n *PlanNode) generatePlanSteps(ctx context.Context, state *graph.AgentGraphState) ([]PlanStep, error) {
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
		msg, err := n.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSitePlan), input)
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
	if _, err := AppendStreamItem(ctx, n.eventStore, streamSinkFromContext(ctx), StreamItem{
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

type durablePlanStore struct {
	store api.PlanRecordStore
}

func NewPlanStore(store api.PlanRecordStore) api.PlanStore {
	if store == nil {
		return nil
	}
	return &durablePlanStore{store: store}
}

func (s *durablePlanStore) OrchestrationPlanStore() {}

func (s *durablePlanStore) LoadPlan(ctx context.Context, sessionID string) (*Plan, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("plan store is not available")
	}
	record, err := s.store.LoadPlanBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return planFromStoreRecord(record), nil
}

func (s *durablePlanStore) SavePlan(ctx context.Context, plan *Plan) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("plan store is not available")
	}
	return s.store.SavePlan(ctx, storeRecordFromPlan(plan))
}

func (s *durablePlanStore) AppendStepEvidence(
	ctx context.Context,
	sessionID string,
	runID string,
	stepID string,
	evidence PlanEvidence,
) (*Plan, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(stepID) == "" {
		return nil, fmt.Errorf("plan step id is required")
	}
	plan, err := s.LoadPlan(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	stepIndex := -1
	for i, step := range plan.Steps {
		if step.ID == stepID {
			stepIndex = i
			break
		}
	}
	if stepIndex < 0 {
		return nil, fmt.Errorf("plan step %s no longer exists", stepID)
	}
	if err := validatePlanEvidence(stepID, evidence); err != nil {
		return nil, err
	}
	plan.Steps[stepIndex].Evidence = append(plan.Steps[stepIndex].Evidence, evidence)
	plan.RunID = strings.TrimSpace(runID)
	plan.UpdatedAt = time.Now().UTC()
	if err := s.SavePlan(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *durablePlanStore) AppendToolResultEvidenceRef(ctx context.Context, resultRef string, ref toolresult.EvidenceRef) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("plan store is not available")
	}
	_, err := s.store.AppendEvidenceRef(ctx, resultRef, ref)
	return err
}

func planFromStoreRecord(record *store.PlanRecord) *Plan {
	if record == nil {
		return nil
	}
	steps := make([]PlanStep, 0, len(record.Steps))
	for _, step := range record.Steps {
		steps = append(steps, PlanStep{
			ID:                 step.ID,
			Action:             step.Action,
			Status:             PlanStepStatus(step.Status),
			DependsOn:          append([]string(nil), step.DependsOn...),
			RepoTargets:        planRepoTargetsFromStore(step.RepoTargets),
			VerificationIntent: verificationIntentsFromStore(step.VerificationIntent),
			Risk:               PlanStepRisk(step.Risk),
			ToolHints:          append([]string(nil), step.ToolHints...),
			Evidence:           planEvidenceFromStore(step.Evidence),
		})
	}
	return &Plan{
		PlanID:    record.PlanID,
		SessionID: record.SessionID,
		RunID:     record.RunID,
		Steps:     steps,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func storeRecordFromPlan(plan *Plan) *store.PlanRecord {
	if plan == nil {
		return nil
	}
	steps := make([]store.PlanStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, store.PlanStep{
			ID:                 step.ID,
			Action:             step.Action,
			Status:             store.PlanStepStatus(step.Status),
			DependsOn:          append([]string(nil), step.DependsOn...),
			RepoTargets:        storePlanRepoTargets(step.RepoTargets),
			VerificationIntent: storeVerificationIntents(step.VerificationIntent),
			Risk:               store.PlanStepRisk(step.Risk),
			ToolHints:          append([]string(nil), step.ToolHints...),
			Evidence:           storePlanEvidence(step.Evidence),
		})
	}
	return &store.PlanRecord{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     steps,
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

func planRepoTargetsFromStore(items []store.PlanRepoTarget) []PlanRepoTarget {
	result := make([]PlanRepoTarget, 0, len(items))
	for _, item := range items {
		result = append(result, PlanRepoTarget{
			Path:       item.Path,
			Symbol:     item.Symbol,
			StartLine:  item.StartLine,
			EndLine:    item.EndLine,
			Reason:     item.Reason,
			Confidence: item.Confidence,
		})
	}
	return result
}

func storePlanRepoTargets(items []PlanRepoTarget) []store.PlanRepoTarget {
	result := make([]store.PlanRepoTarget, 0, len(items))
	for _, item := range items {
		result = append(result, store.PlanRepoTarget{
			Path:       item.Path,
			Symbol:     item.Symbol,
			StartLine:  item.StartLine,
			EndLine:    item.EndLine,
			Reason:     item.Reason,
			Confidence: item.Confidence,
		})
	}
	return result
}

func verificationIntentsFromStore(items []store.VerificationIntent) []VerificationIntent {
	result := make([]VerificationIntent, 0, len(items))
	for _, item := range items {
		result = append(result, VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return result
}

func storeVerificationIntents(items []VerificationIntent) []store.VerificationIntent {
	result := make([]store.VerificationIntent, 0, len(items))
	for _, item := range items {
		result = append(result, store.VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return result
}

func planEvidenceFromStore(items []store.PlanEvidence) []PlanEvidence {
	result := make([]PlanEvidence, 0, len(items))
	for _, item := range items {
		result = append(result, PlanEvidence{
			ID:            item.ID,
			StepID:        item.StepID,
			Kind:          EvidenceKind(item.Kind),
			Status:        EvidenceStatus(item.Status),
			Summary:       item.Summary,
			ToolResultRef: item.ToolResultRef,
			ToolName:      item.ToolName,
			Command:       append([]string(nil), item.Command...),
			Paths:         append([]string(nil), item.Paths...),
			DiffRef:       item.DiffRef,
			ChildRunID:    item.ChildRunID,
			Error:         item.Error,
			SourceRunID:   item.SourceRunID,
			RecordedAt:    item.RecordedAt,
		})
	}
	return result
}

func storePlanEvidence(items []PlanEvidence) []store.PlanEvidence {
	result := make([]store.PlanEvidence, 0, len(items))
	for _, item := range items {
		result = append(result, store.PlanEvidence{
			ID:            item.ID,
			StepID:        item.StepID,
			Kind:          string(item.Kind),
			Status:        string(item.Status),
			Summary:       item.Summary,
			ToolResultRef: item.ToolResultRef,
			ToolName:      item.ToolName,
			Command:       append([]string(nil), item.Command...),
			Paths:         append([]string(nil), item.Paths...),
			DiffRef:       item.DiffRef,
			ChildRunID:    item.ChildRunID,
			Error:         item.Error,
			SourceRunID:   item.SourceRunID,
			RecordedAt:    item.RecordedAt,
		})
	}
	return result
}

func toolVerificationCommand(toolName string, argumentsJSON string) []string {
	if toolName != "run_command" && toolName != "run_verification" {
		return nil
	}
	var payload struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err == nil && len(payload.Command) > 0 {
		return append([]string(nil), payload.Command...)
	}
	return nil
}

func latestEvidenceSummary(items []PlanEvidence) string {
	for i := len(items) - 1; i >= 0; i-- {
		summary := strings.TrimSpace(items[i].Summary)
		if summary != "" {
			return summary
		}
	}
	return ""
}

func validatePlanEvidence(stepID string, evidence PlanEvidence) error {
	if strings.TrimSpace(stepID) == "" {
		return fmt.Errorf("plan step id is required")
	}
	if strings.TrimSpace(evidence.StepID) == "" {
		return fmt.Errorf("plan evidence step_id is required")
	}
	if strings.TrimSpace(evidence.StepID) != strings.TrimSpace(stepID) {
		return fmt.Errorf("plan evidence step_id %q does not match target step %q", evidence.StepID, stepID)
	}
	if strings.TrimSpace(evidence.SourceRunID) == "" {
		return fmt.Errorf("plan evidence source_run_id is required")
	}
	if evidence.RecordedAt.IsZero() {
		return fmt.Errorf("plan evidence recorded_at is required")
	}
	if !validEvidenceKind(evidence.Kind) {
		return fmt.Errorf("plan evidence kind %q is invalid", evidence.Kind)
	}
	if !validEvidenceStatus(evidence.Status) {
		return fmt.Errorf("plan evidence status %q is invalid", evidence.Status)
	}
	switch evidence.Kind {
	case EvidenceKindDiff:
		if strings.TrimSpace(evidence.DiffRef) == "" && len(trimmedNonEmptyStrings(evidence.Paths)) == 0 {
			return fmt.Errorf("plan evidence diff kind requires diff_ref or paths")
		}
	}
	evidence.Command = trimmedNonEmptyStrings(evidence.Command)
	evidence.Paths = trimmedNonEmptyStrings(evidence.Paths)
	return nil
}

func validEvidenceKind(kind EvidenceKind) bool {
	switch kind {
	case EvidenceKindTool, EvidenceKindCommand, EvidenceKindDiff, EvidenceKindCheckpoint, EvidenceKindRollback, EvidenceKindTest, EvidenceKindSubagent, EvidenceKindVerifier, EvidenceKindManual:
		return true
	default:
		return false
	}
}

func validEvidenceStatus(status EvidenceStatus) bool {
	switch status {
	case EvidenceStatusRecorded, EvidenceStatusPassed, EvidenceStatusFailed, EvidenceStatusConfirmed:
		return true
	default:
		return false
	}
}

func trimmedNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func ensureVerificationIntentCoverage(step PlanStep) error {
	if len(step.VerificationIntent) == 0 {
		return nil
	}
	missing := make([]string, 0, len(step.VerificationIntent))
	for _, intent := range step.VerificationIntent {
		if !intentCovered(intent, step.Evidence) {
			missing = append(missing, strings.TrimSpace(intent.Kind))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: step %s missing coverage for %s", ErrPlanStepVerificationGap, strings.TrimSpace(step.ID), strings.Join(missing, ", "))
}

func intentCovered(intent VerificationIntent, evidence []PlanEvidence) bool {
	kind := strings.TrimSpace(intent.Kind)
	for _, item := range evidence {
		if !evidenceCountsForCoverage(item) {
			continue
		}
		switch kind {
		case "read":
			if (item.Kind == EvidenceKindTool && isReadTool(item.ToolName)) || (item.Kind == EvidenceKindManual && item.Status == EvidenceStatusConfirmed) {
				return true
			}
		case "test":
			if item.Kind == EvidenceKindTest {
				return true
			}
			if item.Kind == EvidenceKindCommand && commandMatchesIntent(item, intent) {
				return true
			}
		case "build", "lint":
			if item.Kind == EvidenceKindCommand && commandMatchesIntent(item, intent) {
				return true
			}
		case "diff":
			if item.Kind == EvidenceKindDiff {
				return true
			}
		case "checkpoint":
			if item.Kind == EvidenceKindCheckpoint {
				return true
			}
		case "rollback":
			if item.Kind == EvidenceKindRollback {
				return true
			}
		case "manual":
			if item.Kind == EvidenceKindManual && item.Status == EvidenceStatusConfirmed {
				return true
			}
		case "subagent":
			if item.Kind == EvidenceKindSubagent {
				return true
			}
		case "verifier":
			if item.Kind == EvidenceKindVerifier {
				return true
			}
		}
	}
	return false
}

func verifierEvidenceFromResult(stepID, parentRunID string, result *orchestration.VerificationResult, recordedAt time.Time) PlanEvidence {
	if result == nil {
		reason := "verifier result is nil"
		return PlanEvidence{
			ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
			StepID:      stepID,
			Kind:        EvidenceKindVerifier,
			Status:      EvidenceStatusRecorded,
			Summary:     reason,
			Error:       reason,
			SourceRunID: parentRunID,
			RecordedAt:  recordedAt,
		}
	}
	status := EvidenceStatusRecorded
	errText := ""
	switch result.Verdict {
	case orchestration.VerificationVerdictPassed:
		status = EvidenceStatusPassed
	case orchestration.VerificationVerdictFailed:
		status = EvidenceStatusFailed
		errText = strings.Join(trimmedNonEmptyStrings(result.BlockingFindings), "; ")
		if errText == "" {
			errText = "verifier failed"
		}
	case orchestration.VerificationVerdictInconclusive:
		errText = strings.Join(trimmedNonEmptyStrings(result.MissingEvidence), "; ")
		if errText == "" {
			errText = "verifier inconclusive"
		}
	default:
		errText = fmt.Sprintf("verifier verdict %q is inconclusive", strings.TrimSpace(string(result.Verdict)))
	}
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = fmt.Sprintf("verifier verdict: %s", strings.TrimSpace(string(result.Verdict)))
	}
	return PlanEvidence{
		ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        EvidenceKindVerifier,
		Status:      status,
		Summary:     summary,
		ChildRunID:  strings.TrimSpace(result.ChildRunID),
		Error:       errText,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func evidenceCountsForCoverage(item PlanEvidence) bool {
	return item.Status == EvidenceStatusPassed || item.Status == EvidenceStatusConfirmed
}

func commandMatchesIntent(item PlanEvidence, intent VerificationIntent) bool {
	intentCommand := trimmedNonEmptyStrings(intent.Command)
	itemCommand := trimmedNonEmptyStrings(item.Command)
	if len(intentCommand) > 0 && !slices.Equal(intentCommand, itemCommand) {
		return false
	}
	intentPaths := trimmedNonEmptyStrings(intent.Paths)
	if len(intentPaths) == 0 {
		return true
	}
	itemPaths := trimmedNonEmptyStrings(item.Paths)
	if len(itemPaths) == 0 {
		return false
	}
	for _, path := range intentPaths {
		if slices.Contains(itemPaths, path) {
			return true
		}
	}
	return false
}

func isReadTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff", "git_summary":
		return true
	default:
		return false
	}
}

type toolExecutionRecorder struct {
	items []recordedToolArtifact
}

type recordedToolArtifact struct {
	Kind    EvidenceKind
	Status  EvidenceStatus
	Summary string
	Paths   []string
	DiffRef string
	Error   string
}

type toolMessageEvidenceInput struct {
	Step          PlanStep
	RunID         string
	ToolName      string
	ToolCallID    string
	ArgumentsJSON string
	Message       *planToolMessage
	RecordedAt    time.Time
}

type planToolMessage struct {
	Content string
	Extra   map[string]any
}

func evidenceForToolMessage(input toolMessageEvidenceInput) ([]PlanEvidence, error) {
	items := make([]PlanEvidence, 0, 4)
	if input.Message == nil {
		return items, nil
	}
	resultRef := toolResultRefFromMessage(input.Message.Extra)
	status := EvidenceStatusRecorded
	errText := strings.TrimSpace(toolErrorReason(input.Message))
	if errText != "" {
		status = EvidenceStatusFailed
	}
	base := PlanEvidence{
		ID:            fmt.Sprintf("%s-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:        input.Step.ID,
		Kind:          EvidenceKindTool,
		Status:        status,
		Summary:       ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content),
		ToolResultRef: resultRef,
		ToolName:      input.ToolName,
		Command:       toolVerificationCommand(input.ToolName, input.ArgumentsJSON),
		Paths:         evidencePathsForTool(input.ToolName, input.ArgumentsJSON),
		Error:         errText,
		SourceRunID:   input.RunID,
		RecordedAt:    input.RecordedAt,
	}
	items = append(items, base)

	recorder := recorderFromMessageExtra(input.Message.Extra)
	for idx, item := range recorder.items {
		ev := PlanEvidence{
			ID:          fmt.Sprintf("%s-artifact-%d-%d", input.ToolName, input.RecordedAt.UnixNano(), idx),
			StepID:      input.Step.ID,
			Kind:        item.Kind,
			Status:      item.Status,
			Summary:     strings.TrimSpace(item.Summary),
			ToolName:    input.ToolName,
			Paths:       trimmedNonEmptyStrings(item.Paths),
			DiffRef:     strings.TrimSpace(item.DiffRef),
			Error:       strings.TrimSpace(item.Error),
			SourceRunID: input.RunID,
			RecordedAt:  input.RecordedAt,
		}
		items = append(items, ev)
	}

	if extra, err := commandOrTestEvidence(input, status); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if extra := diffEvidenceFromTool(input); extra != nil {
		items = append(items, *extra)
	}
	if extra, err := delegatedSubagentEvidence(input); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if extra, err := mutationCheckpointEvidence(input); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if extra, err := rollbackEvidenceFromTool(input); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if resultRef != "" {
		for i := range items {
			items[i].ToolResultRef = resultRef
		}
	}
	return items, nil
}

func delegatedSubagentEvidence(input toolMessageEvidenceInput) (*PlanEvidence, error) {
	if input.ToolName != "delegate_task" {
		return nil, nil
	}
	var payload orchestration.ChildAgentResult
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return nil, fmt.Errorf("parse delegate_task result: %w", err)
	}
	childRunID := strings.TrimSpace(payload.ChildRunID)
	if childRunID == "" {
		return nil, fmt.Errorf("parse delegate_task result: child_run_id is required")
	}
	acceptanceStatus := strings.TrimSpace(payload.Acceptance.Status)
	if acceptanceStatus == "" {
		return nil, fmt.Errorf("parse delegate_task result: acceptance.status is required")
	}
	status := EvidenceStatusPassed
	errorText := ""
	summary := fmt.Sprintf("child run %s passed acceptance", childRunID)
	if acceptanceStatus != "passed" {
		status = EvidenceStatusFailed
		errorText = strings.Join(trimmedNonEmptyStrings(payload.Acceptance.Reasons), "; ")
		summary = fmt.Sprintf("child run %s failed acceptance", childRunID)
		if errorText == "" {
			errorText = fmt.Sprintf("child run %s acceptance failed", childRunID)
		}
	}
	return &PlanEvidence{
		ID:          fmt.Sprintf("%s-subagent-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        EvidenceKindSubagent,
		Status:      status,
		Summary:     summary,
		ToolName:    input.ToolName,
		ChildRunID:  childRunID,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func mutationCheckpointEvidence(input toolMessageEvidenceInput) (*PlanEvidence, error) {
	switch input.ToolName {
	case "create_file", "replace_span", "apply_unified_patch", "multi_edit":
	default:
		return nil, nil
	}
	if strings.TrimSpace(toolErrorReason(input.Message)) != "" {
		return nil, nil
	}
	var payload struct {
		CheckpointID    string   `json:"checkpoint_id"`
		CheckpointPaths []string `json:"checkpoint_paths"`
		Path            string   `json:"path"`
		Paths           []string `json:"paths"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return nil, fmt.Errorf("parse %s checkpoint result: %w", input.ToolName, err)
	}
	checkpointID := strings.TrimSpace(payload.CheckpointID)
	if checkpointID == "" {
		return nil, fmt.Errorf("parse %s checkpoint result: checkpoint_id is required", input.ToolName)
	}
	paths := trimmedNonEmptyStrings(payload.CheckpointPaths)
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings(payload.Paths)
	}
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings([]string{payload.Path})
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("parse %s checkpoint result: checkpoint_paths are required", input.ToolName)
	}
	summary := fmt.Sprintf("workspace checkpoint %s recorded for %d path(s)", checkpointID, len(paths))
	return &PlanEvidence{
		ID:          fmt.Sprintf("%s-checkpoint-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        EvidenceKindCheckpoint,
		Status:      EvidenceStatusPassed,
		Summary:     summary,
		ToolName:    input.ToolName,
		Paths:       paths,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func rollbackEvidenceFromTool(input toolMessageEvidenceInput) (*PlanEvidence, error) {
	if input.ToolName != "rollback_workspace_checkpoint" {
		return nil, nil
	}
	var payload struct {
		CheckpointID  string   `json:"checkpoint_id"`
		RollbackID    string   `json:"rollback_id"`
		Status        string   `json:"status"`
		RestoredPaths []string `json:"restored_paths"`
		ConflictPaths []string `json:"conflict_paths"`
		Error         string   `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		if reason := strings.TrimSpace(toolErrorReason(input.Message)); reason != "" {
			return failedRollbackEvidence(input, reason), nil
		}
		return nil, fmt.Errorf("parse rollback_workspace_checkpoint result: %w", err)
	}
	errorText := strings.TrimSpace(payload.Error)
	var status EvidenceStatus
	var summary string
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		rollbackID = strings.TrimSpace(payload.CheckpointID)
	}
	if strings.TrimSpace(payload.Status) == "succeeded" {
		status = EvidenceStatusPassed
		summary = fmt.Sprintf("workspace rollback %s restored %d path(s)", rollbackID, len(trimmedNonEmptyStrings(payload.RestoredPaths)))
		if rollbackID == "" {
			return nil, fmt.Errorf("parse rollback_workspace_checkpoint result: rollback_id is required")
		}
	} else {
		status = EvidenceStatusFailed
		conflicts := trimmedNonEmptyStrings(payload.ConflictPaths)
		if errorText == "" && len(conflicts) > 0 {
			errorText = strings.Join(conflicts, ", ")
		}
		if errorText == "" {
			errorText = "workspace rollback failed"
		}
		if rollbackID == "" {
			rollbackID = "unknown"
		}
		summary = fmt.Sprintf("workspace rollback %s failed", rollbackID)
	}
	paths := trimmedNonEmptyStrings(payload.RestoredPaths)
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings(payload.ConflictPaths)
	}
	return &PlanEvidence{
		ID:          fmt.Sprintf("rollback_workspace_checkpoint-%d", input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        EvidenceKindRollback,
		Status:      status,
		Summary:     summary,
		ToolName:    input.ToolName,
		Paths:       paths,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func failedRollbackEvidence(input toolMessageEvidenceInput, reason string) *PlanEvidence {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "workspace rollback failed"
	}
	return &PlanEvidence{
		ID:          fmt.Sprintf("rollback_workspace_checkpoint-%d", input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        EvidenceKindRollback,
		Status:      EvidenceStatusFailed,
		Summary:     reason,
		ToolName:    input.ToolName,
		Error:       reason,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}
}

func commandOrTestEvidence(input toolMessageEvidenceInput, status EvidenceStatus) (*PlanEvidence, error) {
	if input.ToolName == "run_verification" {
		return runVerificationEvidence(input)
	}
	command := toolVerificationCommand(input.ToolName, input.ArgumentsJSON)
	if len(command) == 0 {
		return nil, nil
	}
	kind := EvidenceKindCommand
	if intentKinds(input.Step.VerificationIntent, "test") {
		kind = EvidenceKindTest
	}
	commandStatus := EvidenceStatusPassed
	errText := strings.TrimSpace(toolErrorReason(input.Message))
	if errText != "" {
		commandStatus = EvidenceStatusFailed
	}
	paths := append([]string(nil), intentPathsForKinds(input.Step.VerificationIntent, "test", "build", "lint")...)
	if len(paths) == 0 {
		paths = commandPathsFromArgs(input.ToolName, input.ArgumentsJSON)
	}
	return &PlanEvidence{
		ID:          fmt.Sprintf("%s-command-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        kind,
		Status:      commandStatus,
		Summary:     ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content),
		ToolName:    input.ToolName,
		Command:     command,
		Paths:       paths,
		Error:       errText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func diffEvidenceFromTool(input toolMessageEvidenceInput) *PlanEvidence {
	if input.ToolName != "inspect_git_diff" && input.ToolName != "git_summary" {
		return nil
	}
	status := EvidenceStatusPassed
	if errText := strings.TrimSpace(toolErrorReason(input.Message)); errText != "" {
		status = EvidenceStatusFailed
	}
	paths := evidencePathsForTool(input.ToolName, input.ArgumentsJSON)
	diffRef := ""
	if input.ToolName == "git_summary" {
		var payload struct {
			DiffArtifactID string   `json:"diff_artifact_id"`
			ChangedPaths   []string `json:"changed_paths"`
		}
		if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err == nil {
			diffRef = strings.TrimSpace(payload.DiffArtifactID)
			if len(paths) == 0 {
				paths = trimmedNonEmptyStrings(payload.ChangedPaths)
			}
		}
		if diffRef == "" {
			return nil
		}
	}
	return &PlanEvidence{
		ID:          fmt.Sprintf("%s-diff-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        EvidenceKindDiff,
		Status:      status,
		Summary:     ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content),
		ToolName:    input.ToolName,
		Paths:       paths,
		DiffRef:     diffRef,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}
}

func runVerificationEvidence(input toolMessageEvidenceInput) (*PlanEvidence, error) {
	var payload struct {
		Kind             string   `json:"kind"`
		Status           string   `json:"status"`
		Summary          string   `json:"summary"`
		Command          []string `json:"command"`
		Paths            []string `json:"paths"`
		StdoutArtifactID string   `json:"stdout_artifact_id"`
		StderrArtifactID string   `json:"stderr_artifact_id"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return nil, fmt.Errorf("parse run_verification result: %w", err)
	}
	command := trimmedNonEmptyStrings(payload.Command)
	if len(command) == 0 {
		command = toolVerificationCommand(input.ToolName, input.ArgumentsJSON)
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("parse run_verification result: command is required")
	}
	kind := EvidenceKindCommand
	if strings.TrimSpace(payload.Kind) == "test" {
		kind = EvidenceKindTest
	}
	evidenceStatus := EvidenceStatusFailed
	errorText := ""
	if strings.TrimSpace(payload.Status) == "passed" {
		evidenceStatus = EvidenceStatusPassed
	} else {
		errorText = strings.TrimSpace(payload.Summary)
		if errorText == "" {
			errorText = fmt.Sprintf("%s verification failed", strings.TrimSpace(payload.Kind))
		}
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content)
	}
	paths := trimmedNonEmptyStrings(payload.Paths)
	if len(paths) == 0 {
		paths = evidencePathsForTool(input.ToolName, input.ArgumentsJSON)
	}
	return &PlanEvidence{
		ID:          fmt.Sprintf("%s-command-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        kind,
		Status:      evidenceStatus,
		Summary:     summary,
		ToolName:    input.ToolName,
		Command:     command,
		Paths:       paths,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func recorderFromMessageExtra(extra map[string]any) toolExecutionRecorder {
	if len(extra) == 0 {
		return toolExecutionRecorder{}
	}
	raw, ok := extra["plan_evidence_recorder"]
	if !ok {
		return toolExecutionRecorder{}
	}
	recorder, ok := raw.(toolExecutionRecorder)
	if ok {
		return recorder
	}
	ptr, ok := raw.(*toolExecutionRecorder)
	if ok && ptr != nil {
		return *ptr
	}
	return toolExecutionRecorder{}
}

func toolResultRefFromMessage(extra map[string]any) string {
	if len(extra) == 0 {
		return ""
	}
	raw, ok := extra["tool_result_ref"]
	if !ok {
		return ""
	}
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func toolErrorReason(msg *planToolMessage) string {
	if msg == nil || msg.Extra == nil {
		return ""
	}
	failed := false
	if rawFailed, ok := msg.Extra["tool_error"]; ok {
		if value, valueOK := rawFailed.(bool); valueOK {
			failed = value
		}
	}
	if !failed {
		return ""
	}
	reason := ""
	if rawReason, ok := msg.Extra["tool_error_reason"]; ok {
		if value, valueOK := rawReason.(string); valueOK {
			reason = value
		}
	}
	if strings.TrimSpace(reason) != "" {
		return reason
	}
	return strings.TrimSpace(msg.Content)
}

func evidencePathsForTool(toolName string, argumentsJSON string) []string {
	var payload struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil
	}
	switch toolName {
	case "create_file", "replace_span", "inspect_git_diff":
		return trimmedNonEmptyStrings([]string{payload.Path})
	case "apply_unified_patch", "git_summary", "run_verification":
		return trimmedNonEmptyStrings(payload.Paths)
	case "multi_edit":
		var multiEdit struct {
			Edits []struct {
				Path string `json:"path"`
			} `json:"edits"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &multiEdit); err != nil {
			return nil
		}
		paths := make([]string, 0, len(multiEdit.Edits))
		for _, edit := range multiEdit.Edits {
			paths = append(paths, edit.Path)
		}
		return trimmedNonEmptyStrings(paths)
	default:
		return nil
	}
}

func commandPathsFromArgs(toolName string, argumentsJSON string) []string {
	if toolName != "run_command" && toolName != "run_verification" {
		return nil
	}
	return evidencePathsForTool(toolName, argumentsJSON)
}

func intentKinds(items []VerificationIntent, want ...string) bool {
	for _, item := range items {
		for _, candidate := range want {
			if strings.TrimSpace(item.Kind) == candidate {
				return true
			}
		}
	}
	return false
}

func intentPathsForKinds(items []VerificationIntent, kinds ...string) []string {
	out := make([]string, 0)
	for _, item := range items {
		for _, kind := range kinds {
			if strings.TrimSpace(item.Kind) != kind {
				continue
			}
			out = append(out, trimmedNonEmptyStrings(item.Paths)...)
		}
	}
	return trimmedNonEmptyStrings(out)
}

var (
	ErrRiskyToolRequiresPlan   = errors.New("risky tool execution requires an active persisted plan")
	ErrPlanStepVerificationGap = errors.New("plan step requires recorded verification before completion")
)

func enforceRiskyToolPlan(ctx context.Context, planStore PlanStore, spec tooling.ToolSpec) (string, string, error) {
	if spec.PlanPolicy != tooling.PlanPolicyRequireActivePlan {
		return "", "", nil
	}
	if planStore == nil {
		return "", "", errors.New("plan enforcement store is not available")
	}
	sessionID := strings.TrimSpace(SessionIDFromContext(ctx))
	if sessionID == "" {
		return "", "", fmt.Errorf("%w: session_id not available for %s", ErrRiskyToolRequiresPlan, spec.Name)
	}
	plan, err := planStore.LoadPlan(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrPlanNotFound) {
			return "", "", fmt.Errorf("%w: active plan not available before %s", ErrRiskyToolRequiresPlan, spec.Name)
		}
		return "", "", fmt.Errorf("load active plan for %s: %w", spec.Name, err)
	}
	stepIndex, err := graph.FindSingleInProgressPlanStep(plan)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrRiskyToolRequiresPlan, err)
	}
	return sessionID, plan.Steps[stepIndex].ID, nil
}

var streamPlanFromDomain = stream.StreamPlanFromDomain
var streamStepPayloadFromPlan = stream.StreamStepPayloadFromPlan
var clonePlanStep = stream.ClonePlanStep

type ExecuteDispatchNode struct {
	store         PlanStore
	eventStore    EventAppender
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
	planStore PlanStore,
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
		wrappedModel, err = wrapModelWithHandlers(ctx, chatModel, handlers)
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
			compose.WithSerializer(&jsonSerializer{}),
		)
	}

	runnable, err := g.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, fmt.Errorf("compile plan-execute graph: %w", err)
	}
	return runnable, nil
}

func NewExecuteDispatchNode(store PlanStore, eventStore EventAppender, childExecutor orchestration.ChildAgentExecutor) *ExecuteDispatchNode {
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
	sessionID := strings.TrimSpace(SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, fmt.Errorf("execute dispatch requires session_id")
	}
	runID := strings.TrimSpace(getRunID(ctx))
	if runID == "" {
		return nil, fmt.Errorf("execute dispatch requires run_id")
	}

	plan, stepIndex, err := n.loadRunnablePlan(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if plan.Steps[stepIndex].Status == PlanStepPending {
		plan.Steps[stepIndex].Status = PlanStepInProgress
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
	var evidence PlanEvidence
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

	plan.Steps[stepIndex].Status = PlanStepCompleted
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
		case PlanStepCompleted:
			line := step.Action
			if summary != "" {
				line = summary
			}
			completed = append(completed, line)
		case PlanStepFailed:
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

func (n *ExecuteDispatchNode) loadRunnablePlan(ctx context.Context, sessionID string) (*Plan, int, error) {
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

func (n *ExecuteDispatchNode) reloadStep(ctx context.Context, sessionID string, stepID string) (*Plan, int, error) {
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

func (n *ExecuteDispatchNode) failStep(ctx context.Context, plan *Plan, stepIndex int, reason string) (*Plan, error) {
	plan.Steps[stepIndex].Status = PlanStepFailed
	plan.UpdatedAt = time.Now().UTC()
	if err := n.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("mark plan step failed: %w", err)
	}
	if err := n.emitStepFailed(ctx, plan, plan.Steps[stepIndex], reason); err != nil {
		return nil, err
	}
	return plan, nil
}

func (n *ExecuteDispatchNode) emitStepStarted(ctx context.Context, plan *Plan, step PlanStep) error {
	if n.eventStore == nil {
		return nil
	}
	_, err := AppendStreamItem(ctx, n.eventStore, streamSinkFromContext(ctx), StreamItem{
		RunID:     plan.RunID,
		Kind:      StreamKindStepStarted,
		CreatedAt: plan.UpdatedAt,
		Payload:   &PlanStepStartedPayload{PlanStepPayload: streamStepPayloadFromPlan(plan, step)},
	})
	if err != nil {
		return fmt.Errorf("append step.started event: %w", err)
	}
	return nil
}

func (n *ExecuteDispatchNode) emitStepCompleted(ctx context.Context, plan *Plan, step PlanStep) error {
	if n.eventStore == nil {
		return nil
	}
	_, err := AppendStreamItem(ctx, n.eventStore, streamSinkFromContext(ctx), StreamItem{
		RunID:     plan.RunID,
		Kind:      StreamKindStepCompleted,
		CreatedAt: plan.UpdatedAt,
		Payload:   &PlanStepCompletedPayload{PlanStepPayload: streamStepPayloadFromPlan(plan, step)},
	})
	if err != nil {
		return fmt.Errorf("append step.completed event: %w", err)
	}
	return nil
}

func (n *ExecuteDispatchNode) emitStepFailed(ctx context.Context, plan *Plan, step PlanStep, reason string) error {
	if n.eventStore == nil {
		return nil
	}
	_, err := AppendStreamItem(ctx, n.eventStore, streamSinkFromContext(ctx), StreamItem{
		RunID:     plan.RunID,
		Kind:      StreamKindStepFailed,
		CreatedAt: plan.UpdatedAt,
		Payload: &PlanStepFailedPayload{
			PlanStepPayload: streamStepPayloadFromPlan(plan, step),
			Error:           reason,
		},
	})
	if err != nil {
		return fmt.Errorf("append step.failed event: %w", err)
	}
	return nil
}

func (n *ExecuteDispatchNode) buildChildRequest(sessionID, runID string, plan *Plan, step PlanStep, messages []*schema.Message) orchestration.ChildAgentRequest {
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
		RequestedMode:    orchestrationmode.SingleAgent,
	}
}

func (n *ExecuteDispatchNode) buildVerificationRequest(sessionID, runID string, plan *Plan, step PlanStep, messages []*schema.Message) orchestration.VerificationRequest {
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

func formatExecuteChildTask(plan *Plan, step PlanStep) string {
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

func completedStepContext(plan *Plan, currentStepID string) string {
	if plan == nil {
		return ""
	}
	lines := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if step.ID == currentStepID || step.Status != PlanStepCompleted {
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

func stepRequiresVerifier(step PlanStep) bool {
	for _, intent := range step.VerificationIntent {
		if strings.TrimSpace(intent.Kind) == "verifier" {
			return true
		}
	}
	return false
}

func verifierAcceptanceCriteria(step PlanStep) []string {
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

func verifierEvidenceRefs(items []PlanEvidence) []string {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if ref := strings.TrimSpace(item.ID); ref != "" {
			refs = append(refs, ref)
		}
	}
	return trimmedNonEmptyStrings(refs)
}

func verifierToolResultRefs(items []PlanEvidence) []string {
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

func subagentEvidenceFromChildResult(stepID, parentRunID string, result *orchestration.ChildAgentResult, recordedAt time.Time) PlanEvidence {
	if result == nil {
		return failedChildExecutionEvidence(stepID, parentRunID, errors.New("child result is nil"), recordedAt)
	}
	summary := summarizeChildResult(result)
	status := EvidenceStatusPassed
	errText := ""
	if strings.TrimSpace(result.Acceptance.Status) != "passed" {
		status = EvidenceStatusFailed
		errText = strings.Join(trimmedNonEmptyStrings(result.Acceptance.Reasons), "; ")
		if errText == "" {
			errText = fmt.Sprintf("child run %s acceptance failed", strings.TrimSpace(result.ChildRunID))
		}
	}
	return PlanEvidence{
		ID:          fmt.Sprintf("subagent-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        EvidenceKindSubagent,
		Status:      status,
		Summary:     summary,
		ChildRunID:  strings.TrimSpace(result.ChildRunID),
		Error:       errText,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func failedChildExecutionEvidence(stepID, parentRunID string, execErr error, recordedAt time.Time) PlanEvidence {
	reason := "child execution failed"
	if execErr != nil && strings.TrimSpace(execErr.Error()) != "" {
		reason = strings.TrimSpace(execErr.Error())
	}
	return PlanEvidence{
		ID:          fmt.Sprintf("subagent-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        EvidenceKindSubagent,
		Status:      EvidenceStatusFailed,
		Summary:     reason,
		Error:       reason,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
}

func failedVerifierExecutionEvidence(stepID, parentRunID string, execErr error, recordedAt time.Time) PlanEvidence {
	reason := "verifier execution failed"
	if execErr != nil && strings.TrimSpace(execErr.Error()) != "" {
		reason = strings.TrimSpace(execErr.Error())
	}
	return PlanEvidence{
		ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        EvidenceKindVerifier,
		Status:      EvidenceStatusFailed,
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

func formatDispatchOutcome(step PlanStep, succeeded bool) string {
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

func failedPlanExecutionEvidenceReason(items []PlanEvidence) (string, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		switch item.Kind {
		case EvidenceKindSubagent:
			if item.Status != EvidenceStatusFailed {
				continue
			}
		case EvidenceKindVerifier:
			if item.Status != EvidenceStatusFailed && !(item.Status == EvidenceStatusRecorded && strings.TrimSpace(item.Error) != "") {
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
