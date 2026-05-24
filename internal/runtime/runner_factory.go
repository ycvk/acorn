package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/terminalsession"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

type orchestrationPlane interface {
	BuildDirectResponse(context.Context, orchestration.DirectResponseRequest) (*orchestration.RunAssembly, error)
	BuildSingleAgent(context.Context, orchestration.SingleAgentRequest) (*orchestration.RunAssembly, error)
	BuildPlanExecute(context.Context, orchestration.PlanExecuteRequest) (*orchestration.RunAssembly, error)
}

type closeableIndexStore interface {
	crystallization.IndexStore
	Close() error
}

type RunnerFactory struct {
	cfg                *config.Config
	store              RunnerFactoryStore
	loader             *skills.Loader
	decisionProfiles   *decision.ProfileService
	checkpointService  *workingstate.Service
	sessionSummarySvc  *runtimehistory.SessionSummaryService
	memoryModule       memorymodule.Service
	memoryModuleErr    error
	contextPlane       contextplane.ContextPlane
	contextPlaneErr    error
	orchestration      orchestrationPlane
	mcpPendingActions  mcpprovider.PendingActionStore
	workspace          *workspace.Workspace
	workspaceErr       error
	artifactService    *artifacts.Service
	artifactServiceErr error
	terminalService    *terminalsession.Service
	terminalServiceErr error
	extraLocalTools    []einotool.BaseTool
	handlers           []adk.ChatModelAgentMiddleware
	mu                 sync.Mutex
	cachedManager      *mcpprovider.Manager
	lastSessionOverlay string

	registry     *Registry
	currentRunID atomic.Value // stores string; atomic to avoid deadlock with f.mu in providerEventCallback

	eventMu     sync.Mutex
	eventErrors map[string]error

	runBuilder   *runBuilder
	crystallizer crystallization.Service
	indexStore   closeableIndexStore
}

const (
	noEligibleSkillMatchReason = "no_eligible_match"
	ambiguousTopScoreReason    = "ambiguous_top_score"
)

func NewRunnerFactory(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) *RunnerFactory {
	deps := initRunnerFactoryDeps(cfg, store, opts)
	return assembleRunnerFactory(cfg, store, opts, deps)
}

func (f *RunnerFactory) New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return f.ensureRunBuilder().Build(ctx, req)
}

func (f *RunnerFactory) BuildCapabilityCatalog(ctx context.Context) (*tooling.Catalog, error) {
	if f == nil || f.cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	childExec := f.newChildAgentExecutor()
	toolset, err := f.buildToolset(ctx, "", childExec, true, tooling.ToolProfileRun)
	if err != nil {
		return nil, err
	}
	return toolset.Catalog(), nil
}

func (f *RunnerFactory) newChildAgentExecutor() *SubagentExecutor {
	return NewSubagentExecutor(f.cfg, f.store, f, nil)
}

func (f *RunnerFactory) cloneForWorkspace(ws *workspace.Workspace) *RunnerFactory {
	if f == nil {
		return nil
	}
	clone := NewRunnerFactory(f.cfg, f.store, RunnerFactoryOptions{
		Loader:                 f.loader,
		DecisionProfileService: decision.NewProfileService(ws.Root()),
		ExtraLocalTools:        append([]einotool.BaseTool(nil), f.extraLocalTools...),
		Workspace:              ws,
		Handlers:               append([]adk.ChatModelAgentMiddleware(nil), f.handlers...),
		CheckpointService:      f.checkpointService,
		SessionSummaryService:  f.sessionSummarySvc,
		MemoryModule:           f.memoryModule,
		ContextPlane:           f.contextPlane,
		MCPPendingActionStore:  f.mcpPendingActions,
	})
	clone.registry = f.registry
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
	return f.cfg
}

func (f *RunnerFactory) MemoryModule() memorymodule.Service {
	if f == nil {
		return nil
	}
	return f.memoryModule
}

func (f *RunnerFactory) SessionSummarySvc() *runtimehistory.SessionSummaryService {
	if f == nil {
		return nil
	}
	return f.sessionSummarySvc
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	if f == nil {
		return nil, errors.New("runner factory is nil")
	}
	return f.newChatModel(ctx)
}

func (f *RunnerFactory) hasWorkingContext(ctx context.Context, sessionID string) (bool, error) {
	if f == nil || f.checkpointService == nil || strings.TrimSpace(sessionID) == "" {
		return false, nil
	}
	checkpoint, err := f.checkpointService.Get(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("load working checkpoint: %w", err)
	}
	if checkpoint == nil {
		return false, nil
	}
	return strings.TrimSpace(checkpoint.Content) != "", nil
}
