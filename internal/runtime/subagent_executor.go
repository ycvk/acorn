package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

type SubagentExecutor struct {
	cfg   *config.Config
	store ExecutorStore
	rf    *RunnerFactory
	ctrl  *RunController

	depthMu sync.Mutex
	depths  map[string]int
}

func NewSubagentExecutor(cfg *config.Config, store ExecutorStore, rf *RunnerFactory, ctrl *RunController) *SubagentExecutor {
	if ctrl == nil {
		ctrl = NewRunController()
	}
	return &SubagentExecutor{
		cfg:    cfg,
		store:  store,
		rf:     rf,
		ctrl:   ctrl,
		depths: make(map[string]int),
	}
}

func (se *SubagentExecutor) currentDepth(parentRunID string) int {
	if se != nil && se.rf != nil && se.rf.registry != nil {
		if rc, ok := se.rf.registry.Get(strings.TrimSpace(parentRunID)); ok {
			return rc.Depth
		}
	}
	se.depthMu.Lock()
	defer se.depthMu.Unlock()
	return se.depths[parentRunID]
}

func (se *SubagentExecutor) incrementDepth(parentRunID string) int {
	se.depthMu.Lock()
	defer se.depthMu.Unlock()
	se.depths[parentRunID]++
	return se.depths[parentRunID]
}

func (se *SubagentExecutor) decrementDepth(parentRunID string) {
	se.depthMu.Lock()
	defer se.depthMu.Unlock()
	if se.depths[parentRunID] > 0 {
		se.depths[parentRunID]--
	}
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

	newDepth := se.incrementDepth(parentRunID)
	defer se.decrementDepth(parentRunID)

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
	childRunnerFactory := se.rf
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
		childRunnerFactory = se.rf.cloneForWorkspace(childWorkspace)
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
		Payload: &stream.SubagentStartedPayload{
			SubRunID:          subRunID,
			ParentID:          parentRunID,
			SessionID:         childSessionID,
			Depth:             newDepth,
			Task:              task,
			ChildRunMode:      string(childRunMode),
			WorkspaceMode:     string(workspaceMode),
			WorktreePath:      worktreePath,
			ContextMessages:   len(req.ContextMessages),
			OrchestrationMode: string(requestedMode),
			ParentStepID:      req.ParentStepID,
		},
	}); err != nil {
		return nil, fmt.Errorf("emit subagent.started: %w", err)
	}

	childCtx := childCtxOrBackground(ctx)

	exec, err := NewExecutorWithRunRuntimeAndController(se.cfg, se.store, childRunnerFactory, se.ctrl)
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
		Depth:             se.currentDepth(parentRunID) + 1,
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
		Payload: &stream.SubagentCompletedPayload{
			SubRunID:          subRunID,
			ParentID:          parentRunID,
			SessionID:         childSessionID,
			Summary:           delegated.OutputSummary,
			FinalStatus:       delegated.FinalStatus,
			AcceptanceStatus:  delegated.Acceptance.Status,
			AcceptanceReasons: append([]string(nil), delegated.Acceptance.Reasons...),
			ChildRunMode:      string(childRunMode),
			WorkspaceMode:     string(workspaceMode),
			WorktreePath:      worktreePath,
			EvidenceRefs:      append([]string(nil), delegated.EvidenceRefs...),
			OrchestrationMode: string(requestedMode),
			ParentStepID:      req.ParentStepID,
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
		Payload: &stream.SubagentFailedPayload{
			SubRunID:          subRunID,
			ParentID:          parentRunID,
			SessionID:         childSessionID,
			Error:             errMsg,
			AcceptanceStatus:  "failed",
			AcceptanceReasons: []string{errMsg},
			ChildRunMode:      string(childRunMode),
			WorkspaceMode:     string(workspaceMode),
			WorktreePath:      worktreePath,
			OrchestrationMode: string(mode),
			ParentStepID:      parentStepID,
		},
	}); err != nil {
		return fmt.Errorf("emit subagent.failed: %w", err)
	}
	return nil
}

func (se *SubagentExecutor) createChildWorktreeWorkspace(ctx context.Context, subRunID string) (*workspace.Workspace, error) {
	if se == nil || se.rf == nil || se.rf.deps.Workspace == nil {
		return nil, errors.New("child worktree requires an initialized workspace")
	}
	worktree, err := se.rf.deps.Workspace.CreateChildWorktree(childCtxOrBackground(ctx), subRunID)
	if err != nil {
		return nil, fmt.Errorf("create child worktree: %w", err)
	}
	childWorkspace, err := se.rf.deps.Workspace.OpenWorktree(worktree)
	if err != nil {
		return nil, fmt.Errorf("open child worktree: %w", err)
	}
	return childWorkspace, nil
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
