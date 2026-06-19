package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/workspace"
)

// defaultMaxSubagentDepth caps plan_execute -> subagent recursion (root = 0)
// when agent.max_subagent_depth is unset. Without a cap a delegating subagent
// that itself delegates can recurse without bound and create worktrees until the
// disk fills. The limit is configurable via agent.max_subagent_depth; exceeding
// it fails loud and the error surfaces to the model as a failed tool result /
// plan evidence.
const defaultMaxSubagentDepth = 3

type SubagentExecutor struct {
	cfg                  *config.Config
	store                ExecutorStore
	runRuntime           RunRuntime
	ctrl                 *RunController
	parentDepth          func(parentRunID string) int
	createChildWorkspace func(context.Context, string) (*workspace.Workspace, error)
	runtimeForWorkspace  func(*workspace.Workspace) RunRuntime
}

type SubagentExecutorOptions struct {
	Config               *config.Config
	Store                ExecutorStore
	RunRuntime           RunRuntime
	Controller           *RunController
	ParentDepth          func(parentRunID string) int
	CreateChildWorkspace func(context.Context, string) (*workspace.Workspace, error)
	RuntimeForWorkspace  func(*workspace.Workspace) RunRuntime
}

func NewSubagentExecutor(opts SubagentExecutorOptions) (*SubagentExecutor, error) {
	if opts.Config == nil {
		return nil, errors.New("config is required")
	}
	if opts.Store == nil {
		return nil, errors.New("store is required")
	}
	if opts.RunRuntime == nil {
		return nil, errors.New("run runtime is required")
	}
	if opts.ParentDepth == nil {
		return nil, errors.New("parent depth resolver is required")
	}
	if opts.CreateChildWorkspace == nil {
		return nil, errors.New("child workspace creator is required")
	}
	if opts.RuntimeForWorkspace == nil {
		return nil, errors.New("workspace runtime factory is required")
	}
	ctrl := opts.Controller
	if ctrl == nil {
		ctrl = NewRunController()
	}
	return &SubagentExecutor{
		cfg:                  opts.Config,
		store:                opts.Store,
		runRuntime:           opts.RunRuntime,
		ctrl:                 ctrl,
		parentDepth:          opts.ParentDepth,
		createChildWorkspace: opts.CreateChildWorkspace,
		runtimeForWorkspace:  opts.RuntimeForWorkspace,
	}, nil
}

func NewSubagentExecutorFactory(cfg *config.Config, store ExecutorStore, ctrl *RunController) ChildAgentExecutorFactory {
	return func(deps ChildAgentRuntimeDeps) (orchestration.ChildAgentExecutor, error) {
		return NewSubagentExecutor(SubagentExecutorOptions{
			Config:               cfg,
			Store:                store,
			RunRuntime:           deps.RunRuntime,
			Controller:           ctrl,
			ParentDepth:          deps.ParentDepth,
			CreateChildWorkspace: deps.CreateChildWorkspace,
			RuntimeForWorkspace:  deps.RuntimeForWorkspace,
		})
	}
}

// currentDepth returns the execution-tree depth of the parent run, sourced
// from the runtime Registry via the injected resolver.
func (se *SubagentExecutor) currentDepth(parentRunID string) int {
	if se == nil || se.parentDepth == nil {
		return 0
	}
	return se.parentDepth(parentRunID)
}

// maxSubagentDepth returns the configured recursion cap, falling back to the
// default when agent.max_subagent_depth is unset (<= 0).
func (se *SubagentExecutor) maxSubagentDepth() int {
	if se != nil && se.cfg != nil && se.cfg.Agent.MaxSubagentDepth > 0 {
		return se.cfg.Agent.MaxSubagentDepth
	}
	return defaultMaxSubagentDepth
}

func (se *SubagentExecutor) Execute(ctx context.Context, req orchestration.ChildAgentRequest) (*orchestration.ChildAgentResult, error) {
	req = normalizeChildAgentRequest(req)
	task := strings.TrimSpace(req.Task)
	parentRunID := strings.TrimSpace(req.ParentRunID)
	if task == "" {
		return nil, errors.New("task is required for subagent execution")
	}
	if parentRunID == "" {
		return nil, errors.New("parent run ID is required for subagent execution")
	}

	newDepth := se.currentDepth(parentRunID) + 1
	if limit := se.maxSubagentDepth(); newDepth > limit {
		return nil, fmt.Errorf("subagent recursion depth %d exceeds configured limit %d (agent.max_subagent_depth) for parent run %s; raise the limit if deeper delegation is intended", newDepth, limit, parentRunID)
	}

	if se.store == nil {
		return nil, errors.New("subagent executor store is not initialized")
	}

	sink := stream.StreamSinkFromContext(ctx)

	subRunID := newRunID()
	childSessionID := "delegate_" + subRunID
	childRunMode := orchestration.NormalizeChildRunMode(req.ChildRunMode)
	requestedMode := events.OrchestrationMode(req.RequestedMode).Normalize()
	if requestedMode == "" {
		requestedMode = events.ModeSingleAgent
	}
	workspaceMode := orchestration.NormalizeChildWorkspaceMode(req.WorkspaceMode)
	childRunRuntime := se.runRuntime
	worktreePath := ""
	if workspaceMode == orchestration.ChildWorkspaceModeWorktree {
		childWorkspace, err := se.createChildWorktreeWorkspace(ctx, subRunID)
		if err != nil {
			if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
				return nil, errors.Join(err, emitErr)
			}
			return nil, err
		}
		worktreePath = childWorkspace.Root()
		if se.runtimeForWorkspace == nil {
			err := errors.New("child workspace runtime factory is not initialized")
			if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
				return nil, errors.Join(err, emitErr)
			}
			return nil, err
		}
		childRunRuntime = se.runtimeForWorkspace(childWorkspace)
	}
	// Delegated child sessions always start fresh, so bootstrap the session and
	// first user turn in one tight path rather than walking the general chat
	// preparation flow.
	turnIndex, err := se.store.CreateFreshSessionTurn(childCtxOrBackground(ctx), childSessionID, truncateTaskTitle(task), task)
	if err != nil {
		return nil, fmt.Errorf("prepare child session turn: %w", err)
	}

	if _, err := stream.AppendStreamItem(ctx, se.store, sink, stream.StreamItem{
		RunID:     parentRunID,
		Kind:      stream.StreamKindSubagentStarted,
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"sub_run_id":         subRunID,
			"parent_id":          parentRunID,
			"session_id":         childSessionID,
			"depth":              newDepth,
			"task":               task,
			"child_run_mode":     string(childRunMode),
			"workspace_mode":     string(workspaceMode),
			"worktree_path":      worktreePath,
			"context_messages":   len(req.ContextMessages),
			"orchestration_mode": string(requestedMode),
			"parent_step_id":     req.ParentStepID,
		},
	}); err != nil {
		return nil, fmt.Errorf("emit subagent.started: %w", err)
	}

	childCtx := childCtxOrBackground(ctx)

	exec, err := NewExecutorWithRunRuntimeAndController(se.cfg, se.store, childRunRuntime, se.ctrl)
	if err != nil {
		if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
			return nil, errors.Join(fmt.Errorf("create subagent executor: %w", err), emitErr)
		}
		return nil, fmt.Errorf("create subagent executor: %w", err)
	}

	childMessages := append([]*schema.Message(nil), req.ContextMessages...)
	childMessages = append(childMessages, schema.UserMessage(task))
	result, err := exec.ExecuteMessages(childCtx, runtimeapi.ExecuteRequest{
		RunID:             subRunID,
		SessionID:         childSessionID,
		TurnIndex:         turnIndex,
		Input:             task,
		Messages:          childMessages,
		AllowedToolNames:  req.AllowedToolNames,
		OrchestrationMode: requestedMode,
		ParentRunID:       parentRunID,
		Depth:             newDepth,
	}, nil)
	if err != nil {
		if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
			return nil, errors.Join(err, emitErr)
		}
		return nil, err
	}

	planRecord, err := se.store.LoadPlanBySession(childCtxOrBackground(ctx), childSessionID)
	if err != nil && !errors.Is(err, store.ErrPlanNotFound) {
		if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
			return nil, errors.Join(fmt.Errorf("load child plan: %w", err), emitErr)
		}
		return nil, fmt.Errorf("load child plan: %w", err)
	}
	if errors.Is(err, store.ErrPlanNotFound) {
		planRecord = nil
	}

	planFailureReasons := delegationPlanFailureReasons(planRecord)
	finalStatus := string(result.Status)
	if len(planFailureReasons) > 0 {
		finalStatus = string(events.RunStatusFailed)
	}
	delegated := &orchestration.ChildAgentResult{
		ChildRunID:         result.RunID,
		ChildSessionID:     childSessionID,
		ChildRunMode:       childRunMode,
		WorkspaceMode:      workspaceMode,
		WorktreePath:       worktreePath,
		FinalStatus:        finalStatus,
		OutputSummary:      strings.TrimSpace(result.Output),
		EvidenceSummaries:  delegationEvidenceSummaries(planRecord),
		EvidenceRefs:       delegationEvidenceRefs(planRecord),
		EffectiveToolNames: append([]string(nil), req.AllowedToolNames...),
	}
	delegated.Acceptance = evaluateDelegationAcceptance(req, events.RunStatus(finalStatus), delegated.OutputSummary, delegated.EvidenceSummaries, planFailureReasons)

	if _, err := stream.AppendStreamItem(ctx, se.store, sink, stream.StreamItem{
		RunID:     parentRunID,
		Kind:      stream.StreamKindSubagentCompleted,
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"sub_run_id":         subRunID,
			"parent_id":          parentRunID,
			"session_id":         childSessionID,
			"summary":            delegated.OutputSummary,
			"final_status":       delegated.FinalStatus,
			"acceptance_status":  delegated.Acceptance.Status,
			"acceptance_reasons": append([]string(nil), delegated.Acceptance.Reasons...),
			"child_run_mode":     string(childRunMode),
			"workspace_mode":     string(workspaceMode),
			"worktree_path":      worktreePath,
			"evidence_refs":      append([]string(nil), delegated.EvidenceRefs...),
			"orchestration_mode": string(requestedMode),
			"parent_step_id":     req.ParentStepID,
		},
	}); err != nil {
		return delegated, fmt.Errorf("emit subagent.completed: %w", err)
	}
	return delegated, nil
}

func (se *SubagentExecutor) emitFailed(ctx context.Context, parentRunID, subRunID, childSessionID, parentStepID string, childRunMode orchestration.ChildRunMode, workspaceMode orchestration.ChildWorkspaceMode, worktreePath string, mode events.OrchestrationMode, errMsg string, sink stream.StreamSink) error {
	if se == nil || se.store == nil {
		return errors.New("emit subagent.failed: store is not initialized")
	}
	if _, err := stream.AppendStreamItem(ctx, se.store, sink, stream.StreamItem{
		RunID:     parentRunID,
		Kind:      stream.StreamKindSubagentFailed,
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"sub_run_id":         subRunID,
			"parent_id":          parentRunID,
			"session_id":         childSessionID,
			"error":              errMsg,
			"acceptance_status":  "failed",
			"acceptance_reasons": []string{errMsg},
			"child_run_mode":     string(childRunMode),
			"workspace_mode":     string(workspaceMode),
			"worktree_path":      worktreePath,
			"orchestration_mode": string(mode),
			"parent_step_id":     parentStepID,
		},
	}); err != nil {
		return fmt.Errorf("emit subagent.failed: %w", err)
	}
	return nil
}

func (se *SubagentExecutor) createChildWorktreeWorkspace(ctx context.Context, subRunID string) (*workspace.Workspace, error) {
	if se == nil || se.createChildWorkspace == nil {
		return nil, errors.New("child worktree requires an initialized workspace")
	}
	return se.createChildWorkspace(childCtxOrBackground(ctx), subRunID)
}

type subagentExecutorAdapter struct {
	exec orchestration.ChildAgentExecutor
}

func (a subagentExecutorAdapter) ExecuteMessages(ctx context.Context, messages []*schema.Message) (string, error) {
	parentRunID := runtimeapi.GetRunID(ctx)
	if strings.TrimSpace(parentRunID) == "" {
		parentRunID = "sampling_parent"
	}

	task := "MCP sampling request"
	if len(messages) > 0 {
		task = messages[len(messages)-1].Content
	}

	result, err := a.exec.Execute(ctx, orchestration.ChildAgentRequest{
		ParentRunID:        parentRunID,
		Task:               task,
		ContextMessages:    append([]*schema.Message(nil), messages...),
		AcceptanceCriteria: []string{"sampling request completed"},
		Origin:             orchestration.ChildAgentOriginMCPSampling,
	})
	if err != nil {
		return "", err
	}
	return result.OutputSummary, nil
}

func normalizeChildAgentRequest(req orchestration.ChildAgentRequest) orchestration.ChildAgentRequest {
	req.ParentRunID = strings.TrimSpace(req.ParentRunID)
	req.ParentSessionID = strings.TrimSpace(req.ParentSessionID)
	req.ParentStepID = strings.TrimSpace(req.ParentStepID)
	req.Task = strings.TrimSpace(req.Task)
	req.AllowedToolNames = normalizeToolNames(req.AllowedToolNames)
	req.AcceptanceCriteria = normalizeToolNames(req.AcceptanceCriteria)
	req.ExpectedEvidence = normalizeToolNames(req.ExpectedEvidence)
	req.ChildRunMode = orchestration.NormalizeChildRunMode(req.ChildRunMode)
	req.WorkspaceMode = orchestration.NormalizeChildWorkspaceMode(req.WorkspaceMode)
	req.RequestedMode = events.OrchestrationMode(req.RequestedMode).Normalize()
	return req
}

func evaluateDelegationAcceptance(
	req orchestration.ChildAgentRequest,
	runStatus events.RunStatus,
	outputSummary string,
	evidenceSummaries []string,
	planFailureReasons []string,
) orchestration.ChildAgentAcceptance {
	reasons := make([]string, 0, len(req.AcceptanceCriteria)+len(req.ExpectedEvidence)+len(planFailureReasons)+1)
	switch runStatus {
	case events.RunStatusSucceeded:
	default:
		reasons = append(reasons, fmt.Sprintf("child run finished with status %s", runStatus))
	}
	reasons = append(reasons, planFailureReasons...)

	for _, expected := range req.ExpectedEvidence {
		if !summaryContains(evidenceSummaries, expected) {
			reasons = append(reasons, fmt.Sprintf("missing expected evidence: %s", expected))
		}
	}
	for _, criterion := range req.AcceptanceCriteria {
		if acceptanceCriterionSatisfied(criterion, outputSummary, evidenceSummaries) {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("acceptance criterion not satisfied: %s", criterion))
	}
	if len(reasons) == 0 {
		return orchestration.ChildAgentAcceptance{Status: "passed"}
	}
	return orchestration.ChildAgentAcceptance{Status: "failed", Reasons: reasons}
}

func delegationPlanFailureReasons(planRecord *model.Plan) []string {
	if planRecord == nil {
		return nil
	}
	reasons := make([]string, 0)
	for _, step := range planRecord.Steps {
		if strings.TrimSpace(string(step.Status)) != string(model.PlanStepFailed) {
			continue
		}
		reason := strings.TrimSpace(latestStoreEvidenceError(step.Evidence))
		if reason == "" {
			action := strings.TrimSpace(step.Action)
			if action == "" {
				action = strings.TrimSpace(step.ID)
			}
			reason = fmt.Sprintf("child plan step %s failed", action)
		}
		reasons = append(reasons, reason)
	}
	return reasons
}

func latestStoreEvidenceError(items []model.PlanEvidence) string {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.TrimSpace(string(items[i].Status)) != string(model.EvidenceStatusFailed) {
			continue
		}
		if errText := strings.TrimSpace(items[i].Error); errText != "" {
			return errText
		}
		if summary := strings.TrimSpace(items[i].Summary); summary != "" {
			return summary
		}
	}
	return ""
}

func delegationEvidenceSummaries(planRecord *model.Plan) []string {
	if planRecord == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(planRecord.Steps))
	for _, step := range planRecord.Steps {
		for _, evidence := range step.Evidence {
			summary := strings.TrimSpace(evidence.Summary)
			if summary == "" {
				continue
			}
			if _, ok := seen[summary]; ok {
				continue
			}
			seen[summary] = struct{}{}
			result = append(result, summary)
		}
	}
	return result
}

func delegationEvidenceRefs(planRecord *model.Plan) []string {
	if planRecord == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, step := range planRecord.Steps {
		for _, evidence := range step.Evidence {
			for _, ref := range childEvidenceRefs(evidence) {
				if _, ok := seen[ref]; ok {
					continue
				}
				seen[ref] = struct{}{}
				result = append(result, ref)
			}
		}
	}
	return result
}

func childEvidenceRefs(evidence model.PlanEvidence) []string {
	refs := make([]string, 0, 3)
	if ref := strings.TrimSpace(evidence.ToolResultRef); ref != "" {
		refs = append(refs, ref)
	}
	if ref := strings.TrimSpace(evidence.ChildRunID); ref != "" {
		refs = append(refs, "run:"+ref)
	}
	if ref := strings.TrimSpace(evidence.ID); ref != "" {
		refs = append(refs, "evidence:"+ref)
	}
	return refs
}

func acceptanceCriterionSatisfied(criterion string, outputSummary string, evidenceSummaries []string) bool {
	expected := strings.TrimSpace(criterion)
	if expected == "" {
		return true
	}
	if strings.Contains(strings.ToLower(outputSummary), strings.ToLower(expected)) {
		return true
	}
	return summaryContains(evidenceSummaries, expected)
}

func summaryContains(summaries []string, expected string) bool {
	target := strings.ToLower(strings.TrimSpace(expected))
	if target == "" {
		return true
	}
	for _, summary := range summaries {
		if strings.Contains(strings.ToLower(strings.TrimSpace(summary)), target) {
			return true
		}
	}
	return false
}

func normalizeToolNames(requested []string) []string {
	seen := make(map[string]struct{}, len(requested))
	result := make([]string, 0, len(requested))
	for _, name := range requested {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func truncateTaskTitle(task string) string {
	title := strings.TrimSpace(task)
	if len(title) <= 80 {
		return title
	}
	return title[:77] + "..."
}

func childCtxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}
