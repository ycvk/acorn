package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/workspace"
)

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

type subagentRunContext struct {
	parentRunID    string
	subRunID       string
	childSessionID string
	parentStepID   string
	childRunMode   orchestration.ChildRunMode
	workspaceMode  orchestration.ChildWorkspaceMode
	worktreePath   string
	requestedMode  events.OrchestrationMode
	sink           stream.StreamSink
	turnIndex      int
}

func (r *subagentRunContext) failWithEmit(ctx context.Context, se *SubagentExecutor, emitMsg string, returnErr error) error {
	if emitErr := se.emitFailed(ctx, r, emitMsg); emitErr != nil {
		return errors.Join(returnErr, emitErr)
	}
	return returnErr
}

func (se *SubagentExecutor) Execute(ctx context.Context, req orchestration.ChildAgentRequest) (*orchestration.ChildAgentResult, error) {
	req = normalizeChildAgentRequest(req)
	task := strings.TrimSpace(req.Task)
	parentRunID := strings.TrimSpace(req.ParentRunID)
	if err := validateSubagentRequest(task, parentRunID); err != nil {
		return nil, err
	}
	run, newDepth, childRuntime, err := se.prepareSubagentExecution(ctx, req, task, parentRunID)
	if err != nil {
		return nil, err
	}
	result, err := se.executeChildRun(ctx, req, run, childRuntime, task, parentRunID, newDepth)
	if err != nil {
		return nil, err
	}
	planRecord, err := se.loadChildPlan(ctx, run)
	if err != nil {
		return nil, run.failWithEmit(ctx, se, err.Error(), fmt.Errorf("load child plan: %w", err))
	}
	return se.finalizeSubagentRun(ctx, req, run, result, planRecord)
}

func (se *SubagentExecutor) prepareSubagentExecution(ctx context.Context, req orchestration.ChildAgentRequest, task, parentRunID string) (*subagentRunContext, int, RunRuntime, error) {
	run, newDepth, err := se.setupSubagentRun(ctx, req, parentRunID)
	if err != nil {
		return nil, 0, nil, err
	}
	childRuntime, err := se.resolveChildRuntime(ctx, run)
	if err != nil {
		return nil, 0, nil, run.failWithEmit(ctx, se, err.Error(), err)
	}
	run.turnIndex, err = se.createChildSessionTurn(ctx, run, task)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("prepare child session turn: %w", err)
	}
	if err := se.emitSubagentStarted(ctx, run, newDepth, task, req); err != nil {
		return nil, 0, nil, err
	}
	return run, newDepth, childRuntime, nil
}

func validateSubagentRequest(task, parentRunID string) error {
	if task == "" {
		return errors.New("task is required for subagent execution")
	}
	if parentRunID == "" {
		return errors.New("parent run ID is required for subagent execution")
	}
	return nil
}

func (se *SubagentExecutor) setupSubagentRun(ctx context.Context, req orchestration.ChildAgentRequest, parentRunID string) (*subagentRunContext, int, error) {
	newDepth := se.currentDepth(parentRunID) + 1
	if limit := se.maxSubagentDepth(); newDepth > limit {
		return nil, 0, fmt.Errorf("subagent recursion depth %d exceeds configured limit %d (agent.max_subagent_depth) for parent run %s; raise the limit if deeper delegation is intended", newDepth, limit, parentRunID)
	}
	if se.store == nil {
		return nil, 0, errors.New("subagent executor store is not initialized")
	}
	subRunID := NewRunID()
	requestedMode := events.OrchestrationMode(req.RequestedMode).Normalize()
	if requestedMode == "" {
		requestedMode = events.ModeSingleAgent
	}
	run := &subagentRunContext{
		parentRunID:    parentRunID,
		subRunID:       subRunID,
		childSessionID: "delegate_" + subRunID,
		parentStepID:   req.ParentStepID,
		childRunMode:   orchestration.NormalizeChildRunMode(req.ChildRunMode),
		workspaceMode:  orchestration.NormalizeChildWorkspaceMode(req.WorkspaceMode),
		requestedMode:  requestedMode,
		sink:           stream.StreamSinkFromContext(ctx),
	}
	return run, newDepth, nil
}

func (se *SubagentExecutor) resolveChildRuntime(ctx context.Context, run *subagentRunContext) (RunRuntime, error) {
	if run.workspaceMode != orchestration.ChildWorkspaceModeWorktree {
		return se.runRuntime, nil
	}
	childWorkspace, err := se.createChildWorktreeWorkspace(ctx, run.subRunID)
	if err != nil {
		return nil, err
	}
	run.worktreePath = childWorkspace.Root()
	if se.runtimeForWorkspace == nil {
		return nil, errors.New("child workspace runtime factory is not initialized")
	}
	return se.runtimeForWorkspace(childWorkspace), nil
}

func (se *SubagentExecutor) createChildSessionTurn(ctx context.Context, run *subagentRunContext, task string) (int, error) {
	turnIndex, err := se.store.CreateFreshSessionTurn(childCtxOrBackground(ctx), run.childSessionID, truncateTaskTitle(task), task)
	if err != nil {
		return 0, err
	}
	return turnIndex, nil
}

func (se *SubagentExecutor) createChildWorktreeWorkspace(ctx context.Context, subRunID string) (*workspace.Workspace, error) {
	if se == nil || se.createChildWorkspace == nil {
		return nil, errors.New("child worktree requires an initialized workspace")
	}
	return se.createChildWorkspace(childCtxOrBackground(ctx), subRunID)
}

func childCtxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}
