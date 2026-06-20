package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/workspace"
)

func (f *RunnerFactory) newChildAgentExecutor() (orchestration.ChildAgentExecutor, error) {
	if f == nil || f.childAgentExecutorFactory == nil {
		return nil, errors.New("child agent executor factory is not initialized")
	}
	childExec, err := f.childAgentExecutorFactory(ChildAgentRuntimeDeps{
		RunRuntime:           f,
		ParentDepth:          f.parentRunDepth,
		CreateChildWorkspace: f.createChildWorkspace,
		RuntimeForWorkspace:  f.runtimeForWorkspace,
	})
	if err != nil {
		return nil, fmt.Errorf("create child agent executor: %w", err)
	}
	if childExec == nil {
		return nil, errors.New("child agent executor factory returned nil")
	}
	return childExec, nil
}

func (f *RunnerFactory) cloneForWorkspace(ws *workspace.Workspace) *RunnerFactory {
	cloneDeps := f.deps.CloneForWorkspace(ws)
	return &RunnerFactory{
		deps:                      cloneDeps,
		registry:                  f.registry,
		runChatModelBuilder:       f.runChatModelBuilder,
		childAgentExecutorFactory: f.childAgentExecutorFactory,
	}
}

func (f *RunnerFactory) parentRunDepth(parentRunID string) int {
	if f == nil || f.registry == nil {
		return 0
	}
	if rc, ok := f.registry.Get(strings.TrimSpace(parentRunID)); ok {
		return rc.Depth
	}
	return 0
}

func (f *RunnerFactory) createChildWorkspace(ctx context.Context, subRunID string) (*workspace.Workspace, error) {
	if f == nil || f.deps.Workspace == nil {
		return nil, errors.New("child worktree requires an initialized workspace")
	}
	worktree, err := f.deps.Workspace.CreateChildWorktree(ctx, subRunID)
	if err != nil {
		return nil, fmt.Errorf("create child worktree: %w", err)
	}
	childWorkspace, err := f.deps.Workspace.OpenWorktree(worktree)
	if err != nil {
		return nil, fmt.Errorf("open child worktree: %w", err)
	}
	return childWorkspace, nil
}

func (f *RunnerFactory) runtimeForWorkspace(ws *workspace.Workspace) RunRuntime {
	return f.cloneForWorkspace(ws)
}
