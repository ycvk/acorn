package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workspace"
)

type RunnerFactory struct {
	deps RuntimeDeps

	mu                 sync.Mutex
	cachedManager      *mcpprovider.Manager
	lastSessionOverlay string

	registry     *Registry
	currentRunID atomic.Value

	runChatModelBuilder       func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
	childAgentExecutorFactory ChildAgentExecutorFactory
}

type ChildAgentExecutorFactory func(ChildAgentRuntimeDeps) (orchestration.ChildAgentExecutor, error)

type ChildAgentRuntimeDeps struct {
	RunRuntime           RunRuntime
	ParentDepth          func(parentRunID string) int
	CreateChildWorkspace func(context.Context, string) (*workspace.Workspace, error)
	RuntimeForWorkspace  func(*workspace.Workspace) RunRuntime
}

const (
	noEligibleSkillMatchReason = "no_eligible_match"
	ambiguousTopScoreReason    = "ambiguous_top_score"
)

func NewRunnerFactory(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (*RunnerFactory, error) {
	if opts.ChildAgentExecutorFactory == nil {
		return nil, errors.New("child agent executor factory is required")
	}
	deps, err := buildRuntimeDeps(cfg, store, opts)
	if err != nil {
		return nil, fmt.Errorf("build runtime deps: %w", err)
	}
	factory := assembleRunnerFactory(deps)
	factory.childAgentExecutorFactory = opts.ChildAgentExecutorFactory
	return factory, nil
}

func (f *RunnerFactory) New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	return f.buildRun(ctx, req)
}

func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]tooling.ToolSpec, error) {
	childExec, err := f.newChildAgentExecutor()
	if err != nil {
		return nil, err
	}
	toolset, err := f.buildToolset(ctx, "", childExec, true, tooling.ToolProfileRun)
	if err != nil {
		return nil, err
	}
	specs := toolset.Catalog().Specs()
	for i := range specs {
		specs[i].Tool = nil
	}
	if err := toolset.Close(); err != nil {
		return nil, fmt.Errorf("close capability toolset: %w", err)
	}
	return specs, nil
}

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
	clone := &RunnerFactory{
		deps:                      cloneDeps,
		registry:                  f.registry,
		runChatModelBuilder:       f.runChatModelBuilder,
		childAgentExecutorFactory: f.childAgentExecutorFactory,
	}
	return clone
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

func (f *RunnerFactory) Registry() *Registry {
	return f.registry
}

func (f *RunnerFactory) Config() *config.Config {
	return f.deps.Config
}

func (f *RunnerFactory) MemoryModule() memorymodule.Service {
	return f.deps.MemoryModule
}

func (f *RunnerFactory) SessionSummarySvc() *model.SessionSummaryService {
	return f.deps.SessionSummarySvc
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	return f.newChatModel(ctx)
}

func (f *RunnerFactory) hasWorkingContext(ctx context.Context, sessionID string) (bool, error) {
	if strings.TrimSpace(sessionID) == "" || f.deps.CheckpointService == nil {
		return false, nil
	}
	checkpoint, err := f.deps.CheckpointService.Get(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("load working checkpoint: %w", err)
	}
	if checkpoint == nil {
		return false, nil
	}
	return strings.TrimSpace(checkpoint.Content) != "", nil
}

func (r *ActiveRunner) Close() error {
	var closeErr error
	if r.CloseRunTools != nil {
		closeErr = r.CloseRunTools()
		r.CloseRunTools = nil
	}
	if r.Factory != nil && r.RunID != "" {
		r.Factory.registry.Clear(r.RunID)
		r.Factory.ClearCurrentRunID(r.RunID)
	}
	return closeErr
}

func (f *RunnerFactory) setCurrentRunID(runID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentRunID.Store(runID)
}

func (f *RunnerFactory) ClearCurrentRunID(runID string) {
	if runID == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.currentRunIDValue() == runID {
		f.currentRunID.Store("")
	}
}

func (f *RunnerFactory) currentRunIDValue() string {
	value := f.currentRunID.Load()
	runID, ok := value.(string)
	if !ok {
		return ""
	}
	return runID
}
