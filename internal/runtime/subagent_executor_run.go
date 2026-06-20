package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
)

func (se *SubagentExecutor) emitSubagentStarted(ctx context.Context, run *subagentRunContext, newDepth int, task string, req orchestration.ChildAgentRequest) error {
	if _, err := stream.AppendStreamItem(ctx, se.store, run.sink, stream.StreamItem{
		RunID:     run.parentRunID,
		Kind:      stream.StreamKindSubagentStarted,
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"sub_run_id":         run.subRunID,
			"parent_id":          run.parentRunID,
			"session_id":         run.childSessionID,
			"depth":              newDepth,
			"task":               task,
			"child_run_mode":     string(run.childRunMode),
			"workspace_mode":     string(run.workspaceMode),
			"worktree_path":      run.worktreePath,
			"context_messages":   len(req.ContextMessages),
			"orchestration_mode": string(run.requestedMode),
			"parent_step_id":     run.parentStepID,
		},
	}); err != nil {
		return fmt.Errorf("emit subagent.started: %w", err)
	}
	return nil
}

func (se *SubagentExecutor) executeChildRun(ctx context.Context, req orchestration.ChildAgentRequest, run *subagentRunContext, childRuntime RunRuntime, task, parentRunID string, newDepth int) (*Result, error) {
	childCtx := childCtxOrBackground(ctx)
	exec, err := NewExecutorWithRunRuntimeAndController(se.cfg, se.store, childRuntime, se.ctrl)
	if err != nil {
		return nil, run.failWithEmit(ctx, se, err.Error(), fmt.Errorf("create subagent executor: %w", err))
	}
	childMessages := append([]*schema.Message(nil), req.ContextMessages...)
	childMessages = append(childMessages, schema.UserMessage(task))
	result, err := exec.ExecuteMessages(childCtx, runtimeapi.ExecuteRequest{
		RunID:             run.subRunID,
		SessionID:         run.childSessionID,
		TurnIndex:         run.turnIndex,
		Input:             task,
		Messages:          childMessages,
		AllowedToolNames:  req.AllowedToolNames,
		OrchestrationMode: run.requestedMode,
		ParentRunID:       parentRunID,
		Depth:             newDepth,
	}, nil)
	if err != nil {
		return nil, run.failWithEmit(ctx, se, err.Error(), err)
	}
	return result, nil
}

func (se *SubagentExecutor) loadChildPlan(ctx context.Context, run *subagentRunContext) (*model.Plan, error) {
	planRecord, err := se.store.LoadPlanBySession(childCtxOrBackground(ctx), run.childSessionID)
	if err != nil && !errors.Is(err, store.ErrPlanNotFound) {
		return nil, err
	}
	if errors.Is(err, store.ErrPlanNotFound) {
		return nil, nil
	}
	return planRecord, nil
}

func (se *SubagentExecutor) finalizeSubagentRun(ctx context.Context, req orchestration.ChildAgentRequest, run *subagentRunContext, result *Result, planRecord *model.Plan) (*orchestration.ChildAgentResult, error) {
	planFailureReasons := delegationPlanFailureReasons(planRecord)
	finalStatus := string(result.Status)
	if len(planFailureReasons) > 0 {
		finalStatus = string(events.RunStatusFailed)
	}
	delegated := &orchestration.ChildAgentResult{
		ChildRunID:         result.RunID,
		ChildSessionID:     run.childSessionID,
		ChildRunMode:       run.childRunMode,
		WorkspaceMode:      run.workspaceMode,
		WorktreePath:       run.worktreePath,
		FinalStatus:        finalStatus,
		OutputSummary:      strings.TrimSpace(result.Output),
		EvidenceSummaries:  delegationEvidenceSummaries(planRecord),
		EvidenceRefs:       delegationEvidenceRefs(planRecord),
		EffectiveToolNames: append([]string(nil), req.AllowedToolNames...),
	}
	delegated.Acceptance = evaluateDelegationAcceptance(req, events.RunStatus(finalStatus), delegated.OutputSummary, delegated.EvidenceSummaries, planFailureReasons)
	if err := se.emitSubagentCompleted(ctx, run, delegated); err != nil {
		return delegated, err
	}
	return delegated, nil
}

func (se *SubagentExecutor) emitSubagentCompleted(ctx context.Context, run *subagentRunContext, delegated *orchestration.ChildAgentResult) error {
	if _, err := stream.AppendStreamItem(ctx, se.store, run.sink, stream.StreamItem{
		RunID:     run.parentRunID,
		Kind:      stream.StreamKindSubagentCompleted,
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"sub_run_id":         run.subRunID,
			"parent_id":          run.parentRunID,
			"session_id":         run.childSessionID,
			"summary":            delegated.OutputSummary,
			"final_status":       delegated.FinalStatus,
			"acceptance_status":  delegated.Acceptance.Status,
			"acceptance_reasons": append([]string(nil), delegated.Acceptance.Reasons...),
			"child_run_mode":     string(run.childRunMode),
			"workspace_mode":     string(run.workspaceMode),
			"worktree_path":      run.worktreePath,
			"evidence_refs":      append([]string(nil), delegated.EvidenceRefs...),
			"orchestration_mode": string(run.requestedMode),
			"parent_step_id":     run.parentStepID,
		},
	}); err != nil {
		return fmt.Errorf("emit subagent.completed: %w", err)
	}
	return nil
}

func (se *SubagentExecutor) emitFailed(ctx context.Context, run *subagentRunContext, errMsg string) error {
	if se == nil || se.store == nil {
		return errors.New("emit subagent.failed: store is not initialized")
	}
	if _, err := stream.AppendStreamItem(ctx, se.store, run.sink, stream.StreamItem{
		RunID:     run.parentRunID,
		Kind:      stream.StreamKindSubagentFailed,
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"sub_run_id":         run.subRunID,
			"parent_id":          run.parentRunID,
			"session_id":         run.childSessionID,
			"error":              errMsg,
			"acceptance_status":  "failed",
			"acceptance_reasons": []string{errMsg},
			"child_run_mode":     string(run.childRunMode),
			"workspace_mode":     string(run.workspaceMode),
			"worktree_path":      run.worktreePath,
			"orchestration_mode": string(run.requestedMode),
			"parent_step_id":     run.parentStepID,
		},
	}); err != nil {
		return fmt.Errorf("emit subagent.failed: %w", err)
	}
	return nil
}
