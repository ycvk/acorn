package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	ws := opts.Workspace
	var wsErr error
	if ws == nil && cfg != nil {
		ws, wsErr = cfg.Workspace()
	}
	artifactService, artifactServiceErr := buildArtifactService(cfg, store)
	terminalService, terminalServiceErr := buildTerminalSessionService(store, artifactService, artifactServiceErr)
	loader := opts.Loader
	if loader == nil {
		loader = skills.NewLoader(cfg)
	}
	decisionProfiles := opts.DecisionProfileService
	if decisionProfiles == nil {
		root := ""
		if ws != nil {
			root = ws.Root()
		}
		decisionProfiles = decision.NewProfileService(root)
	}
	memoryModule := opts.MemoryModule
	var memoryModuleErr error
	if memoryModule == nil {
		memoryModuleErr = errors.New("memory module is required")
	}
	contextPlane := opts.ContextPlane
	var contextPlaneErr error
	if contextPlane == nil {
		memoryBudget := 0
		maxContextTokens := 0
		var tokenCounter contextplane.TokenCounter
		if cfg != nil {
			memoryBudget = cfg.Memory.Search.TokenBudget
			contextPolicy, err := cfg.ContextPolicy()
			if err != nil {
				contextPlaneErr = fmt.Errorf("context policy: %w", err)
			} else {
				maxContextTokens, err = contextplane.ContextAssemblyTokenLimitFromContextPolicy(contextPolicy)
				if err != nil {
					contextPlaneErr = fmt.Errorf("context plane budget: %w", err)
				} else {
					tokenCounter, err = contextplane.NewCompressionTokenCounter(contextPolicy)
					if err != nil {
						contextPlaneErr = fmt.Errorf("context plane token counter: %w", err)
					}
				}
			}
		}
		contextPlane = contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
			MemorySearchTokenBudget: memoryBudget,
			MaxContextTokens:        maxContextTokens,
			TokenCounter:            tokenCounter,
			Store:                   store,
			CheckpointService:       opts.CheckpointService,
			SessionSummaryService:   opts.SessionSummaryService,
			ToolResultLedger:        store,
		})
	}
	orchestrationPlane := newDefaultOrchestrationPlane(defaultOrchestrationPlaneDeps{
		cfg:          cfg,
		store:        store,
		contextPlane: contextPlane,
		handlers:     opts.Handlers,
	})
	factory := &RunnerFactory{
		cfg:                cfg,
		store:              store,
		loader:             loader,
		decisionProfiles:   decisionProfiles,
		checkpointService:  opts.CheckpointService,
		sessionSummarySvc:  opts.SessionSummaryService,
		memoryModule:       memoryModule,
		memoryModuleErr:    memoryModuleErr,
		contextPlane:       contextPlane,
		contextPlaneErr:    contextPlaneErr,
		orchestration:      orchestrationPlane,
		mcpPendingActions:  opts.MCPPendingActionStore,
		workspace:          ws,
		workspaceErr:       wsErr,
		artifactService:    artifactService,
		artifactServiceErr: artifactServiceErr,
		terminalService:    terminalService,
		terminalServiceErr: terminalServiceErr,
		extraLocalTools:    append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		handlers:           append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
		registry:           NewRegistry(),
	}
	factory.runBuilder = newRunBuilder(factory)
	if memoryModule != nil && cfg != nil && os.Getenv("ACORN_AUTO_CRYSTALLIZATION") == "true" {
		indexStore, err := crystallization.OpenIndexStore(filepath.Join(cfg.Runtime.StorageDir, "insight_index.db"))
		if err == nil {
			factory.crystallizer = crystallization.NewDefaultService(memoryModule, indexStore)
			factory.indexStore = indexStore
		} else {
			factory.memoryModuleErr = errors.Join(factory.memoryModuleErr, fmt.Errorf("open insight index: %w", err))
		}
	}
	return factory
}

func buildArtifactService(cfg *config.Config, store RunnerFactoryStore) (*artifacts.Service, error) {
	if cfg == nil {
		return nil, errors.New("artifact service requires config")
	}
	if strings.TrimSpace(cfg.Runtime.StorageDir) == "" {
		return nil, errors.New("artifact service requires runtime storage_dir")
	}
	artifactStore, ok := store.(artifacts.Store)
	if !ok {
		return nil, errors.New("artifact service requires artifact store")
	}
	return artifacts.NewService(filepath.Join(cfg.Runtime.StorageDir, "artifacts"), artifactStore)
}

func buildTerminalSessionService(store RunnerFactoryStore, artifactService *artifacts.Service, artifactErr error) (*terminalsession.Service, error) {
	if artifactErr != nil {
		return nil, fmt.Errorf("terminal session service requires artifact service: %w", artifactErr)
	}
	terminalStore, ok := store.(terminalsession.Store)
	if !ok {
		return nil, errors.New("terminal session service requires terminal session store")
	}
	return terminalsession.NewService(terminalStore, artifactService)
}

func (f *RunnerFactory) Crystallizer() crystallization.Service {
	if f == nil {
		return nil
	}
	return f.crystallizer
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
