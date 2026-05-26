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
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtimehistory"
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

	eventMu     sync.Mutex
	eventErrors map[string]error

	runChatModelBuilder func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
}

const (
	noEligibleSkillMatchReason = "no_eligible_match"
	ambiguousTopScoreReason    = "ambiguous_top_score"
)

func NewRunnerFactory(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (*RunnerFactory, error) {
	deps, err := buildRuntimeDeps(cfg, store, opts)
	if err != nil {
		return nil, fmt.Errorf("build runtime deps: %w", err)
	}
	return assembleRunnerFactory(deps), nil
}

func (f *RunnerFactory) New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRunCoordinator(f).Build(ctx, req)
}

func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]tooling.ToolSpec, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	childExec := f.newChildAgentExecutor()
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

func (f *RunnerFactory) newChildAgentExecutor() *SubagentExecutor {
	return NewSubagentExecutor(f.deps.Config, f.deps.Store, f, nil)
}

func (f *RunnerFactory) cloneForWorkspace(ws *workspace.Workspace) *RunnerFactory {
	if f == nil {
		return nil
	}
	cloneDeps := f.deps.CloneForWorkspace(ws)
	clone := &RunnerFactory{
		deps:                cloneDeps,
		registry:            f.registry,
		runChatModelBuilder: f.runChatModelBuilder,
	}
	return clone
}

func (f *RunnerFactory) Registry() *Registry {
	if f == nil {
		return nil
	}
	return f.registry
}

func (f *RunnerFactory) ConsumeEventError(runID string) error {
	return f.consumeEventError(runID)
}

func (f *RunnerFactory) Config() *config.Config {
	if f == nil {
		return nil
	}
	return f.deps.Config
}

func (f *RunnerFactory) MemoryModule() memorymodule.Service {
	if f == nil {
		return nil
	}
	return f.deps.MemoryModule
}

func (f *RunnerFactory) SessionSummarySvc() *runtimehistory.SessionSummaryService {
	if f == nil {
		return nil
	}
	return f.deps.SessionSummarySvc
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	if f == nil {
		return nil, errors.New("runner factory is nil")
	}
	return f.newChatModel(ctx)
}

func (f *RunnerFactory) Crystallizer() crystallization.Service {
	if f == nil {
		return nil
	}
	return f.deps.Crystallizer
}

func (f *RunnerFactory) hasWorkingContext(ctx context.Context, sessionID string) (bool, error) {
	if f == nil || f.deps.CheckpointService == nil || strings.TrimSpace(sessionID) == "" {
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
	if r == nil {
		return nil
	}
	var closeErr error
	if r != nil && r.CloseRunTools != nil {
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
	if f == nil || runID == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.currentRunIDValue() == runID {
		f.currentRunID.Store("")
	}
}

func (f *RunnerFactory) currentRunIDValue() string {
	if f == nil {
		return ""
	}
	value := f.currentRunID.Load()
	runID, ok := value.(string)
	if !ok {
		return ""
	}
	return runID
}
