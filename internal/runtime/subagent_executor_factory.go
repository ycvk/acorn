package runtime

import (
	"errors"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/orchestration"
)

func NewSubagentExecutor(opts SubagentExecutorOptions) (*SubagentExecutor, error) {
	if err := validateSubagentExecutorOptions(opts); err != nil {
		return nil, err
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

func validateSubagentExecutorOptions(opts SubagentExecutorOptions) error {
	if opts.Config == nil {
		return errors.New("config is required")
	}
	if opts.Store == nil {
		return errors.New("store is required")
	}
	if opts.RunRuntime == nil {
		return errors.New("run runtime is required")
	}
	if opts.ParentDepth == nil {
		return errors.New("parent depth resolver is required")
	}
	if opts.CreateChildWorkspace == nil {
		return errors.New("child workspace creator is required")
	}
	if opts.RuntimeForWorkspace == nil {
		return errors.New("workspace runtime factory is required")
	}
	return nil
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

func (se *SubagentExecutor) currentDepth(parentRunID string) int {
	if se == nil || se.parentDepth == nil {
		return 0
	}
	return se.parentDepth(parentRunID)
}

func (se *SubagentExecutor) maxSubagentDepth() int {
	if se != nil && se.cfg != nil && se.cfg.Agent.MaxSubagentDepth > 0 {
		return se.cfg.Agent.MaxSubagentDepth
	}
	return defaultMaxSubagentDepth
}
