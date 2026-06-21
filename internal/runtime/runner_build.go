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
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/runtime/toolset"
	"github.com/ycvk/acorn/internal/skills"
	corestore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workspace"
)

// --- runtime deps construction ---

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
		Store:             store,
		Loader:            loader,
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
	tokenCounter, err = contextplane.NewCompressionTokenCounter()
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

// --- assembly dispatch ---

// buildAssembly dispatches to the direct_response orchestration plane,
// reusing the common baseAssemblyFields helper so agent/session/tool fields
// are not duplicated across request constructors.
func (f *RunnerFactory) buildAssembly(
	ctx context.Context,
	mode events.OrchestrationMode,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	if f == nil || f.deps.Orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	bf := f.baseAssemblyFields(req, catalog, chatModel, contextResult)
	return f.deps.Orchestration.BuildDirectResponse(ctx, f.directResponseRequest(bf, req))
}

type baseAssemblyFields struct {
	agentName         string
	agentDescription  string
	sessionID         string
	runID             string
	chatModel         einomodel.BaseChatModel
	catalog           *tooling.Catalog
	contextResult     orchestration.AssembleResultView
	allowedToolNames  []string
	excludedToolNames []string
}

func (f *RunnerFactory) baseAssemblyFields(req RunnerBuildRequest, catalog *tooling.Catalog, chatModel einomodel.BaseChatModel, contextResult *contextplane.AssembleResult) baseAssemblyFields {
	return baseAssemblyFields{
		agentName:         f.deps.Config.Agent.Name,
		agentDescription:  f.deps.Config.Agent.Description,
		sessionID:         req.SessionID,
		runID:             req.RunID,
		chatModel:         chatModel,
		catalog:           catalog,
		contextResult:     AssembleResultToView(contextResult),
		allowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		excludedToolNames: append([]string(nil), req.ExcludedToolNames...),
	}
}

func (f *RunnerFactory) directResponseRequest(bf baseAssemblyFields, req RunnerBuildRequest) orchestration.DirectResponseRequest {
	return orchestration.DirectResponseRequest{
		AgentName:         bf.agentName,
		AgentDescription:  bf.agentDescription,
		SessionID:         bf.sessionID,
		RunID:             bf.runID,
		ChatModel:         bf.chatModel,
		AssistantStreamer: tool.NewDirectAssistantStreamer(f.deps.Store),
		Catalog:           bf.catalog,
		ContextResult:     bf.contextResult,
		AllowedToolNames:  bf.allowedToolNames,
		ExcludedToolNames: bf.excludedToolNames,
		InstructionSuffix: req.InstructionSuffix,
	}
}

// --- run capabilities ---

type runCapabilities struct {
	catalog       *tooling.Catalog
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

func (f *RunnerFactory) buildRunCapabilities(ctx context.Context, sessionID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	toolset, err := f.buildRunToolset(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = toolset.Close()
		}
	}()
	catalog, err := f.assembleRunCapabilitiesCatalog(ctx, toolset, mcpManager)
	if err != nil {
		return nil, err
	}
	skillSnapshot, err := loadStableSkillSnapshot(ctx, f.deps.Loader, skillEligibilityContextFromCatalog(catalog))
	if err != nil {
		return nil, err
	}
	return &runCapabilities{
		catalog:       catalog,
		skillSnapshot: skillSnapshot,
		stableSkills:  stableSkillsFromSnapshot(skillSnapshot),
		close:         toolset.Close,
	}, nil
}

func (f *RunnerFactory) assembleRunCapabilitiesCatalog(ctx context.Context, toolset *toolset.Toolset, mcpManager *mcpprovider.Manager) (*tooling.Catalog, error) {
	specs := append([]tooling.ToolSpec(nil), toolset.Catalog().Specs()...)
	mcpSpecs, err := f.buildMCPToolSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	return tooling.NewCatalog(ctx, specs)
}
