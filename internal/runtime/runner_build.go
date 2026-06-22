package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime/orchestration"
	"github.com/ycvk/acorn/internal/skills"
	corestore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolkit"
	"github.com/ycvk/acorn/internal/workspace"
)

type chatModelBuilder func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error)

func buildRuntimeChatModel(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, error) {
	model, _, err := buildRuntimeChatModelWithProvider(ctx, cfg, newModel)
	return model, err
}

func buildRuntimeChatModelWithProvider(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, config.ProviderConfig, error) {
	if cfg == nil {
		return nil, config.ProviderConfig{}, errors.New("config is required")
	}
	if newModel == nil {
		newModel = newOpenAIChatModel
	}

	provider, err := cfg.EnabledProvider()
	if err != nil {
		return nil, config.ProviderConfig{}, err
	}
	model, err := newModel(ctx, provider)
	if err != nil {
		return nil, config.ProviderConfig{}, fmt.Errorf("init provider %s: %w", provider.Name, err)
	}
	return model, provider, nil
}

func newRuntimeChatModel(
	ctx context.Context,
	cfg *config.Config,
	newModel chatModelBuilder,
	_ any,
) (einomodel.BaseChatModel, error) {
	return buildRuntimeChatModel(ctx, cfg, newModel)
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

func buildAssembleRequest(req RunnerBuildRequest, caps *runCapabilities, selection *runSelection, memoryPrepared *memorymodule.PrepareResult) contextplane.AssembleRequest {
	var selectedSkill *SelectedSkill
	if selection != nil {
		selectedSkill = selection.selectedSkill
	}
	return contextplane.AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SelectedSkill:  selectedSkill,
		SkillSnapshot:  caps.skillSnapshot,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    caps.catalog,
	}
}

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
	contextPlane, err := resolveContextPlane(cfg, store, opts)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("context plane: %w", err)
	}
	orchestrationPlane := newDefaultOrchestrationPlane(defaultOrchestrationPlaneDeps{
		cfg: cfg, store: store, contextPlane: contextPlane, handlers: opts.Handlers,
	})
	return assembleRuntimeDeps(cfg, store, opts, ws, loader, artifactService, contextPlane, orchestrationPlane), nil
}

func resolveLoader(cfg *config.Config, loader *skills.Loader) *skills.Loader {
	if loader == nil {
		return skills.NewLoader(cfg)
	}
	return loader
}

func resolveContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (contextplane.ContextPlane, error) {
	if opts.ContextPlane != nil {
		return opts.ContextPlane, nil
	}
	return buildDefaultContextPlane(cfg, store, opts)
}

func assembleRuntimeDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions, ws *workspace.Workspace, loader *skills.Loader, artifactService *corestore.ArtifactService, contextPlane contextplane.ContextPlane, orchestrationPlane orchestrationPlane) RuntimeDeps {
	return RuntimeDeps{
		Config:            cfg,
		Loader:            loader,
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
	}), nil
}

func resolveContextPlaneTokenPolicy(cfg *config.Config) (memoryBudget, maxContextTokens int, tokenCounter contextplane.TokenCounter, err error) {
	if cfg == nil {
		return 0, 0, nil, nil
	}
	memoryBudget = cfg.Memory.Search.MemoryContextTokenBudget
	maxContextTokens = cfg.Context.WindowTokens
	tokenCounter, err = contextplane.NewTokenCounter()
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

type baseAssemblyFields struct {
	agentName         string
	agentDescription  string
	sessionID         string
	runID             string
	chatModel         einomodel.BaseChatModel
	catalog           *toolkit.Catalog
	contextResult     orchestration.AssembleResultView
	allowedToolNames  []string
	excludedToolNames []string
}

type runCapabilities struct {
	catalog       *toolkit.Catalog
	skillSnapshot *skills.Snapshot
	stableSkills  []skills.Spec
	close         func() error
}

func (c *runCapabilities) Close() error {
	if c == nil || c.close == nil {
		return nil
	}
	return c.close()
}
