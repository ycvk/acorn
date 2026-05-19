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

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
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
	store              runnerFactoryStore
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
	extraLocalTools    []einotool.BaseTool
	handlers           []adk.ChatModelAgentMiddleware
	mu                 sync.Mutex
	cachedManager      *mcpprovider.Manager
	lastSessionOverlay string

	registry     *runRegistry
	currentRunID atomic.Value // stores string; atomic to avoid deadlock with f.mu in providerEventCallback

	eventMu     sync.Mutex
	eventErrors map[string]error

	runBuilder   *runBuilder
	crystallizer crystallization.Service
	indexStore   closeableIndexStore
}

type RunnerFactoryOptions struct {
	Loader                 *skills.Loader
	DecisionProfileService *decision.ProfileService
	ExtraLocalTools        []einotool.BaseTool
	Workspace              *workspace.Workspace
	Handlers               []adk.ChatModelAgentMiddleware
	CheckpointService      *workingstate.Service
	SessionSummaryService  *runtimehistory.SessionSummaryService
	MemoryModule           memorymodule.Service
	ContextPlane           contextplane.ContextPlane
	MCPPendingActionStore  mcpprovider.PendingActionStore
}

type RunnerBuildRequest struct {
	SessionID         string
	RunID             string
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Sink              StreamSink
	ExcludedToolNames []string
	InstructionSuffix string
	OrchestrationMode orchestration.OrchestrationMode
	ParentRunID       string
}

type ActiveRunner struct {
	mcp              *mcpprovider.Manager
	runner           *adk.Runner
	selectedSkill    *SelectedSkill
	instruction      string
	chatModel        einomodel.BaseChatModel
	factory          *RunnerFactory
	contextResult    *contextplane.AssembleResult
	contextSession   contextplane.ContextSession
	runID            string
	compressionState *contextplane.CompressionState
	toolCatalog      *tooling.Catalog
}

const (
	noEligibleSkillMatchReason = "no_eligible_match"
	ambiguousTopScoreReason    = "ambiguous_top_score"
)

func NewRunnerFactory(cfg *config.Config, store runnerFactoryStore, opts RunnerFactoryOptions) *RunnerFactory {
	ws := opts.Workspace
	var wsErr error
	if ws == nil && cfg != nil {
		ws, wsErr = cfg.Workspace()
	}
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
		cfg:               cfg,
		store:             store,
		loader:            loader,
		decisionProfiles:  decisionProfiles,
		checkpointService: opts.CheckpointService,
		sessionSummarySvc: opts.SessionSummaryService,
		memoryModule:      memoryModule,
		memoryModuleErr:   memoryModuleErr,
		contextPlane:      contextPlane,
		contextPlaneErr:   contextPlaneErr,
		orchestration:     orchestrationPlane,
		mcpPendingActions: opts.MCPPendingActionStore,
		workspace:         ws,
		workspaceErr:      wsErr,
		extraLocalTools:   append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		handlers:          append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
		registry:          newRunRegistry(),
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
