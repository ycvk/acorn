package runtime

import (
	"context"
	"errors"
	"fmt"
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
