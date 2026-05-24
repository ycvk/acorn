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

func buildRuntimeDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (RuntimeDeps, error) {
	if cfg == nil {
		return RuntimeDeps{}, errors.New("config is required")
	}
	if store == nil {
		return RuntimeDeps{}, errors.New("store is required")
	}

	ws, err := resolveWorkspace(cfg, opts.Workspace)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("workspace: %w", err)
	}

	artifactService, err := buildArtifactService(cfg, store)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("artifact service: %w", err)
	}

	terminalService, err := buildTerminalSessionService(store, artifactService)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("terminal session service: %w", err)
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

	if opts.MemoryModule == nil {
		return RuntimeDeps{}, errors.New("memory module is required")
	}

	contextPlane := opts.ContextPlane
	if contextPlane == nil {
		contextPlane, err = buildDefaultContextPlane(cfg, store, opts)
		if err != nil {
			return RuntimeDeps{}, fmt.Errorf("context plane: %w", err)
		}
	}

	orchestrationPlane := newDefaultOrchestrationPlane(defaultOrchestrationPlaneDeps{
		cfg:          cfg,
		store:        store,
		contextPlane: contextPlane,
		handlers:     opts.Handlers,
	})

	crystallizer, indexStore, err := buildCrystallizer(opts.MemoryModule, cfg)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("crystallizer: %w", err)
	}

	return RuntimeDeps{
		Config:            cfg,
		Store:             store,
		Loader:            loader,
		DecisionProfiles:  decisionProfiles,
		CheckpointService: opts.CheckpointService,
		SessionSummarySvc: opts.SessionSummaryService,
		MemoryModule:      opts.MemoryModule,
		ContextPlane:      contextPlane,
		Orchestration:     orchestrationPlane,
		MCPPendingActions: opts.MCPPendingActionStore,
		Workspace:         ws,
		ArtifactService:   artifactService,
		TerminalService:   terminalService,
		ExtraLocalTools:   append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		Handlers:          append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
		Crystallizer:      crystallizer,
		IndexStore:        indexStore,
	}, nil
}

func resolveWorkspace(cfg *config.Config, override *workspace.Workspace) (*workspace.Workspace, error) {
	if override != nil {
		return override, nil
	}
	return cfg.Workspace()
}

func buildArtifactService(cfg *config.Config, store RunnerFactoryStore) (*artifacts.Service, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if strings.TrimSpace(cfg.Runtime.StorageDir) == "" {
		return nil, errors.New("storage_dir is required")
	}
	artifactStore, ok := store.(artifacts.Store)
	if !ok {
		return nil, errors.New("store must implement artifacts.Store")
	}
	return artifacts.NewService(filepath.Join(cfg.Runtime.StorageDir, "artifacts"), artifactStore)
}

func buildTerminalSessionService(store RunnerFactoryStore, artifactService *artifacts.Service) (*terminalsession.Service, error) {
	terminalStore, ok := store.(terminalsession.Store)
	if !ok {
		return nil, errors.New("store must implement terminalsession.Store")
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
			return nil, fmt.Errorf("token limit: %w", err)
		}
		tokenCounter, err = contextplane.NewCompressionTokenCounter(contextPolicy)
		if err != nil {
			return nil, fmt.Errorf("token counter: %w", err)
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
		MemoryBudget: contextplane.LayeredMemoryBudget{
			L1IndexTokens:     cfg.Memory.Search.IndexTokenBudget,
			L2InitialTokens:   cfg.Memory.Search.InitialTokenBudget,
			L3OnDemandReserve: cfg.Memory.Search.OnDemandReserve,
		},
	}), nil
}

func buildCrystallizer(memoryModule memorymodule.Service, cfg *config.Config) (crystallization.Service, closeableIndexStore, error) {
	if memoryModule == nil || cfg == nil || os.Getenv("ACORN_AUTO_CRYSTALLIZATION") != "true" {
		return nil, nil, nil
	}
	indexStore, err := crystallization.OpenIndexStore(filepath.Join(cfg.Runtime.StorageDir, "insight_index.db"))
	if err != nil {
		return nil, nil, fmt.Errorf("open insight index: %w", err)
	}
	crystallizer := crystallization.NewDefaultService(memoryModule, indexStore)
	return crystallizer, indexStore, nil
}

func assembleRunnerFactory(deps RuntimeDeps) *RunnerFactory {
	factory := &RunnerFactory{
		deps:     deps,
		registry: NewRegistry(),
	}
	factory.runBuilder = newRunBuilder(factory)
	return factory
}
