package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/terminalsession"
	"github.com/ycvk/acorn/internal/workspace"
)

// runnerFactoryInitDeps holds all dependencies constructed during RunnerFactory initialization.
type runnerFactoryInitDeps struct {
	workspace          *workspace.Workspace
	workspaceErr       error
	artifactService    *artifacts.Service
	artifactServiceErr error
	terminalService    *terminalsession.Service
	terminalServiceErr error
	loader             *skills.Loader
	decisionProfiles   *decision.ProfileService
	memoryModule       memorymodule.Service
	memoryModuleErr    error
	contextPlane       contextplane.ContextPlane
	contextPlaneErr    error
	orchestrationPlane orchestrationPlane
}

// initRunnerFactoryDeps builds all optional dependencies for a RunnerFactory.
// It centralizes the initialization logic so that runner_factory.go can focus
// on runtime management.
func initRunnerFactoryDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) runnerFactoryInitDeps {
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
		contextPlane, contextPlaneErr = buildDefaultContextPlane(cfg, store, opts)
	}

	orchestrationPlane := newDefaultOrchestrationPlane(defaultOrchestrationPlaneDeps{
		cfg:          cfg,
		store:        store,
		contextPlane: contextPlane,
		handlers:     opts.Handlers,
	})

	return runnerFactoryInitDeps{
		workspace:          ws,
		workspaceErr:       wsErr,
		artifactService:    artifactService,
		artifactServiceErr: artifactServiceErr,
		terminalService:    terminalService,
		terminalServiceErr: terminalServiceErr,
		loader:             loader,
		decisionProfiles:   decisionProfiles,
		memoryModule:       memoryModule,
		memoryModuleErr:    memoryModuleErr,
		contextPlane:       contextPlane,
		contextPlaneErr:    contextPlaneErr,
		orchestrationPlane: orchestrationPlane,
	}
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

func buildDefaultContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (contextplane.ContextPlane, error) {
	memoryBudget := 0
	maxContextTokens := 0
	var tokenCounter contextplane.TokenCounter
	if cfg != nil {
		memoryBudget = cfg.Memory.Search.MemoryContextTokenBudget
		contextPolicy, err := cfg.ContextPolicy()
		if err != nil {
			return nil, fmt.Errorf("context policy: %w", err)
		}
		maxContextTokens, err = contextplane.ContextAssemblyTokenLimitFromContextPolicy(contextPolicy)
		if err != nil {
			return nil, fmt.Errorf("context plane budget: %w", err)
		}
		tokenCounter, err = contextplane.NewCompressionTokenCounter(contextPolicy)
		if err != nil {
			return nil, fmt.Errorf("context plane token counter: %w", err)
		}
	}
	return contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: memoryBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             tokenCounter,
		Store:                    store,
		CheckpointService:        opts.CheckpointService,
		SessionSummaryService:    opts.SessionSummaryService,
		ToolResultLedger:         store,
	}), nil
}

// Crystallizer returns the optional crystallization service.
func (f *RunnerFactory) Crystallizer() crystallization.Service {
	if f == nil {
		return nil
	}
	return f.crystallizer
}

// initCrystallizer sets up the optional crystallization service.
func initCrystallizer(f *RunnerFactory) {
	if f.memoryModule == nil || f.cfg == nil || os.Getenv("ACORN_AUTO_CRYSTALLIZATION") != "true" {
		return
	}
	indexStore, err := crystallization.OpenIndexStore(filepath.Join(f.cfg.Runtime.StorageDir, "insight_index.db"))
	if err == nil {
		f.crystallizer = crystallization.NewDefaultService(f.memoryModule, indexStore)
		f.indexStore = indexStore
	} else {
		f.memoryModuleErr = errors.Join(f.memoryModuleErr, fmt.Errorf("open insight index: %w", err))
	}
}

// assembleRunnerFactory creates a RunnerFactory from pre-built dependencies.
func assembleRunnerFactory(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions, deps runnerFactoryInitDeps) *RunnerFactory {
	factory := &RunnerFactory{
		cfg:                cfg,
		store:              store,
		loader:             deps.loader,
		decisionProfiles:   deps.decisionProfiles,
		checkpointService:  opts.CheckpointService,
		sessionSummarySvc:  opts.SessionSummaryService,
		memoryModule:       deps.memoryModule,
		memoryModuleErr:    deps.memoryModuleErr,
		contextPlane:       deps.contextPlane,
		contextPlaneErr:    deps.contextPlaneErr,
		orchestration:      deps.orchestrationPlane,
		mcpPendingActions:  opts.MCPPendingActionStore,
		workspace:          deps.workspace,
		workspaceErr:       deps.workspaceErr,
		artifactService:    deps.artifactService,
		artifactServiceErr: deps.artifactServiceErr,
		terminalService:    deps.terminalService,
		terminalServiceErr: deps.terminalServiceErr,
		extraLocalTools:    append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		handlers:           append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
		registry:           NewRegistry(),
	}
	factory.runBuilder = newRunBuilder(factory)
	initCrystallizer(factory)
	return factory
}
