package runtime

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/skills"
	corestore "github.com/ycvk/acorn/internal/store"
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
	if opts.MemoryModule == nil {
		return RuntimeDeps{}, errors.New("memory module is required")
	}
	loader := resolveLoader(cfg, opts.Loader)
	decisionProfiles := resolveDecisionProfiles(opts.DecisionProfileService, ws)
	contextPlane, err := resolveContextPlane(cfg, store, opts)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("context plane: %w", err)
	}
	orchestrationPlane := newDefaultOrchestrationPlane(defaultOrchestrationPlaneDeps{
		cfg: cfg, store: store, contextPlane: contextPlane, handlers: opts.Handlers,
	})
	return assembleRuntimeDeps(cfg, store, opts, ws, loader, decisionProfiles, artifactService, contextPlane, orchestrationPlane), nil
}

func resolveLoader(cfg *config.Config, loader *skills.Loader) *skills.Loader {
	if loader == nil {
		return skills.NewLoader(cfg)
	}
	return loader
}

func resolveDecisionProfiles(svc *decision.ProfileService, ws *workspace.Workspace) *decision.ProfileService {
	if svc != nil {
		return svc
	}
	root := ""
	if ws != nil {
		root = ws.Root()
	}
	return decision.NewProfileService(root)
}

func resolveContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (contextplane.ContextPlane, error) {
	if opts.ContextPlane != nil {
		return opts.ContextPlane, nil
	}
	return buildDefaultContextPlane(cfg, store, opts)
}

func assembleRuntimeDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions, ws *workspace.Workspace, loader *skills.Loader, decisionProfiles *decision.ProfileService, artifactService *corestore.ArtifactService, contextPlane contextplane.ContextPlane, orchestrationPlane orchestrationPlane) RuntimeDeps {
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
		ExtraLocalTools:   append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		Handlers:          append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
	}
}

func resolveWorkspace(cfg *config.Config, override *workspace.Workspace) (*workspace.Workspace, error) {
	if override != nil {
		return override, nil
	}
	return cfg.Workspace()
}

func buildArtifactService(cfg *config.Config, store RunnerFactoryStore) (*corestore.ArtifactService, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if strings.TrimSpace(cfg.Runtime.StorageDir) == "" {
		return nil, errors.New("storage_dir is required")
	}
	artifactStore, ok := store.(corestore.ArtifactStore)
	if !ok {
		return nil, errors.New("store must implement corestore.ArtifactStore")
	}
	return corestore.NewArtifactService(filepath.Join(cfg.Runtime.StorageDir, "artifacts"), artifactStore)
}

func buildDefaultContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (contextplane.ContextPlane, error) {
	memoryBudget, maxContextTokens, tokenCounter, err := resolveContextPlaneTokenPolicy(cfg)
	if err != nil {
		return nil, err
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

func resolveContextPlaneTokenPolicy(cfg *config.Config) (memoryBudget, maxContextTokens int, tokenCounter contextplane.TokenCounter, err error) {
	if cfg == nil {
		return 0, 0, nil, nil
	}
	memoryBudget = cfg.Memory.Search.MemoryContextTokenBudget
	contextPolicy, policyErr := cfg.ContextPolicy()
	if policyErr != nil {
		return 0, 0, nil, fmt.Errorf("context policy: %w", policyErr)
	}
	maxContextTokens, err = contextplane.ContextAssemblyTokenLimitFromContextPolicy(contextPolicy)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("token limit: %w", err)
	}
	tokenCounter, err = contextplane.NewCompressionTokenCounter(contextPolicy)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("token counter: %w", err)
	}
	return memoryBudget, maxContextTokens, tokenCounter, nil
}

func assembleRunnerFactory(deps RuntimeDeps) *RunnerFactory {
	return &RunnerFactory{
		deps:     deps,
		registry: NewRegistry(),
	}
}
