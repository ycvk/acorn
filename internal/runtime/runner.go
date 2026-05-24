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
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/browser"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	appmodel "github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skilllifecycle"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/terminalsession"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workingstate"
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

	runBuilder *runBuilder
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
	return f.ensureRunBuilder().Build(ctx, req)
}

func (f *RunnerFactory) BuildCapabilityCatalog(ctx context.Context) (*tooling.Catalog, error) {
	if f == nil || f.deps.Config == nil {
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
	return NewSubagentExecutor(f.deps.Config, f.deps.Store, f, nil)
}

func (f *RunnerFactory) cloneForWorkspace(ws *workspace.Workspace) *RunnerFactory {
	if f == nil {
		return nil
	}
	cloneDeps := f.deps.CloneForWorkspace(ws)
	clone := &RunnerFactory{
		deps:     cloneDeps,
		registry: f.registry,
	}
	clone.runBuilder = newRunBuilder(clone)
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

type runSelection struct {
	decisionRecord *decision.Record
	selectedSkill  *SelectedSkill
	hint           *decision.DecisionContextHint
}

func (f *RunnerFactory) buildRunCapabilities(ctx context.Context, sessionID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	childExec := f.newChildAgentExecutor()
	toolset, err := f.buildRunToolset(ctx, sessionID, childExec)
	if err != nil {
		return nil, err
	}
	specs := append([]tooling.ToolSpec(nil), toolset.Catalog().Specs()...)
	var resourceTools []einotool.BaseTool
	var promptTools []einotool.BaseTool
	if mcpManager != nil {
		resourceTools = mcpManager.ResourceTools()
		promptTools = mcpManager.PromptTools()
	}

	for _, registration := range mcpManagerRegistrations(mcpManager) {
		info, err := registration.Tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read MCP tool info for provider %q: %w", registration.ProviderName, err)
		}
		namespaced, err := newMCPNamespacedTool(ctx, registration.Tool, registration.ProviderName, info.Name)
		if err != nil {
			return nil, fmt.Errorf("namespace MCP tool %q for provider %q: %w", info.Name, registration.ProviderName, err)
		}
		spec, err := runtimeToolSpec(ctx, f.deps.Config, registration.ProviderName, tooling.ToolKindMCP, []tooling.ToolProfile{tooling.ToolProfileRun}, namespaced)
		if err != nil {
			return nil, err
		}
		parallelPolicy, err := mcpToolParallelPolicy(f.deps.Config, registration.ProviderName)
		if err != nil {
			return nil, fmt.Errorf("resolve MCP tool safety for provider %q: %w", registration.ProviderName, err)
		}
		spec.Execution.ParallelPolicy = parallelPolicy
		specs = append(specs, spec)
	}
	resourceSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "mcp.resource", tooling.ToolKindMCPResource, []tooling.ToolProfile{tooling.ToolProfileRun}, resourceTools)
	if err != nil {
		return nil, err
	}
	promptSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "mcp.prompt", tooling.ToolKindMCPPrompt, []tooling.ToolProfile{tooling.ToolProfileRun}, promptTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, resourceSpecs...)
	specs = append(specs, promptSpecs...)

	catalog, err := tooling.NewCatalog(ctx, specs)
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

func mcpManagerRegistrations(manager *mcpprovider.Manager) []mcpprovider.ToolRegistration {
	if manager == nil {
		return nil
	}
	return manager.Registrations()
}

func (f *RunnerFactory) resolveRunSelection(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	chatModel einomodel.BaseChatModel,
) (*runSelection, error) {
	if caps == nil {
		return nil, fmt.Errorf("run capabilities are required")
	}

	if strings.TrimSpace(req.Input) == "" && strings.TrimSpace(req.SkillID) == "" && strings.TrimSpace(req.RunID) == "" {
		return &runSelection{}, nil
	}

	if strings.TrimSpace(req.Input) != "" || strings.TrimSpace(req.SkillID) != "" {
		engine, parsed, err := buildPlaneEngine(f.deps.DecisionProfiles)
		if err != nil {
			return nil, err
		}
		hasWorkingContext, err := f.hasWorkingContext(ctx, req.SessionID)
		if err != nil {
			return nil, err
		}
		retrieved, err := skills.RetrieveCandidates(skills.CandidateQuery{
			Input:           req.Input,
			ExplicitSkillID: req.SkillID,
			Eligibility:     skillEligibilityContextFromCatalog(caps.catalog),
		}, caps.stableSkills)
		var discovered []SkillMatch
		if retrieved != nil {
			discovered = runtimeMatchesFromRecommendations(retrieved.Candidates)
		}
		if err != nil {
			return nil, err
		}
		planeReq := buildPlaneRequest(req, caps, discovered, hasWorkingContext)
		record, err := engine.Decide(ctx, decision.DecideInput(planeReq))
		if err != nil {
			return nil, err
		}
		fillRecordMetadata(record, parsed.Hash)
		if err := f.deps.Store.SaveRunDecision(ctx, *record); err != nil {
			return nil, err
		}
		if err := emitDecisionEvents(ctx, f.deps.Store, req, record, req.SkillID); err != nil {
			return nil, err
		}
		result := &decision.Result{
			Record:        record,
			SelectedSkill: selectedSkillResultFromRecord(record),
			Hint:          decision.BuildHint(record.Action),
		}
		enrichSelectedSkillFromMatches(result, discovered, caps.stableSkills)
		selectedSkill, err := selectedSkillFromPlaneResult(result, caps.stableSkills)
		if err != nil {
			return nil, err
		}
		if emitErr := emitSkillSelectionEvents(ctx, f.deps.Store, req, selectedSkill, discovered); emitErr != nil {
			return nil, emitErr
		}
		if !decision.IsContinuableAction(record.Action) {
			switch record.Action {
			case decision.ActionAskUser:
				return nil, fmt.Errorf("decision requires operator confirmation: %s", record.DecisionReason)
			case decision.ActionBlock:
				return nil, fmt.Errorf("decision blocked execution: %s", record.DecisionReason)
			case decision.ActionResumeRun:
				return nil, fmt.Errorf("decision resolved to resume_run for a new execution")
			default:
				return nil, fmt.Errorf("decision action %q is not continuable", record.Action)
			}
		}
		return &runSelection{
			decisionRecord: result.Record,
			selectedSkill:  selectedSkill,
			hint:           result.Hint,
		}, nil
	} else if strings.TrimSpace(req.RunID) != "" {
		var err error
		decisionRecord, err := f.deps.Store.LoadRunDecision(ctx, req.RunID)
		if err != nil {
			return nil, err
		}
		if decisionRecord == nil {
			return nil, fmt.Errorf("run decision missing for %s", req.RunID)
		}
		result := &decision.Result{
			Record:        decisionRecord,
			SelectedSkill: selectedSkillResultFromRecord(decisionRecord),
		}
		enrichSelectedSkillFromMatches(result, nil, caps.stableSkills)
		selectedSkill, err := selectedSkillFromPlaneResult(result, caps.stableSkills)
		if err != nil {
			return nil, err
		}
		return &runSelection{
			decisionRecord: decisionRecord,
			selectedSkill:  selectedSkill,
			hint:           decision.BuildHint(decisionRecord.Action),
		}, nil
	}
	return &runSelection{}, nil
}

func (f *RunnerFactory) buildSingleAgentAssembly(
	ctx context.Context,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	if f == nil || f.deps.Orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	instructionSuffix, err := f.withMemoryInstruction(ctx, req)
	if err != nil {
		return nil, err
	}
	return f.deps.Orchestration.BuildSingleAgent(ctx, orchestration.SingleAgentRequest{
		AgentName:         f.deps.Config.Agent.Name,
		AgentDescription:  f.deps.Config.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		AssistantStreamer: newDirectAssistantStreamer(f.deps.Store),
		Catalog:           catalog,
		ContextResult:     AssembleResultToView(contextResult),
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		ExcludedToolNames: append([]string(nil), req.ExcludedToolNames...),
		InstructionSuffix: instructionSuffix,
	})
}

func (f *RunnerFactory) buildDirectResponseAssembly(
	ctx context.Context,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	if f == nil || f.deps.Orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	return f.deps.Orchestration.BuildDirectResponse(ctx, orchestration.DirectResponseRequest{
		AgentName:         f.deps.Config.Agent.Name,
		AgentDescription:  f.deps.Config.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		AssistantStreamer: newDirectAssistantStreamer(f.deps.Store),
		Catalog:           catalog,
		ContextResult:     AssembleResultToView(contextResult),
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		ExcludedToolNames: append([]string(nil), req.ExcludedToolNames...),
		InstructionSuffix: req.InstructionSuffix,
	})
}

func (f *RunnerFactory) buildPlanExecuteAssembly(
	ctx context.Context,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	if f == nil || f.deps.Orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	instructionSuffix, err := f.withMemoryInstruction(ctx, req)
	if err != nil {
		return nil, err
	}
	return f.deps.Orchestration.BuildPlanExecute(ctx, orchestration.PlanExecuteRequest{
		AgentName:         f.deps.Config.Agent.Name,
		AgentDescription:  f.deps.Config.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		Catalog:           catalog,
		ContextResult:     AssembleResultToView(contextResult),
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		ExcludedToolNames: append([]string(nil), req.ExcludedToolNames...),
		InstructionSuffix: instructionSuffix,
		ChildExecutor:     f.newChildAgentExecutor(),
	})
}

func (f *RunnerFactory) withMemoryInstruction(ctx context.Context, req RunnerBuildRequest) (string, error) {
	if f == nil || f.deps.MemoryModule == nil {
		return "", errors.New("memory module is not initialized")
	}
	workspaceSlug := ""
	if f.deps.Workspace != nil {
		workspaceSlug = memorymodule.WorkspaceSlug(f.deps.Workspace.Root())
	}
	instruction, err := f.deps.MemoryModule.BuildMemoryInstruction(ctx, workspaceSlug)
	if err != nil {
		return "", fmt.Errorf("build memory instruction: %w", err)
	}
	return buildStableInstruction(req.InstructionSuffix, instruction), nil
}

type modelProviderAssembler struct {
	factory  *RunnerFactory
	buildRun func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
}

func (a *modelProviderAssembler) BuildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
	if a == nil || a.factory == nil || a.factory.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if a.buildRun != nil {
		return a.buildRun(ctx, req)
	}
	model, provider, err := buildRuntimeChatModelWithProvider(ctx, a.factory.deps.Config, nil)
	if err != nil {
		return nil, err
	}
	metadata := providerusage.RunMetadata{
		RunID:        req.RunID,
		SessionID:    req.SessionID,
		ProviderName: provider.Name,
		ModelName:    provider.Model,
	}
	if req.RunID != "" && a.factory.deps.Store != nil {
		existing, err := a.factory.deps.Store.ListProviderUsagesByRun(ctx, req.RunID)
		if err == nil {
			metadata.InitialSequence = uint64(len(existing))
		}
	}
	return providerusage.WrapModel(model, a.factory.deps.Store, metadata)
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

type capabilityAssembler struct {
	factory *RunnerFactory
}

func (a *capabilityAssembler) BuildRunCapabilities(ctx context.Context, req RunnerBuildRequest) (*capabilityAssembly, error) {
	if a == nil || a.factory == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mcpManager, err := a.factory.bootstrapRunMCP(ctx, req)
	if err != nil {
		return nil, err
	}
	capabilities, err := a.factory.buildRunCapabilities(ctx, req.SessionID, mcpManager)
	if err != nil {
		return nil, err
	}
	return &capabilityAssembly{mcpManager: mcpManager, capabilities: capabilities}, nil
}

type contextSelectionAssembler struct {
	factory *RunnerFactory
}

func (a *contextSelectionAssembler) PrepareMemory(ctx context.Context, req RunnerBuildRequest) (*memorymodule.PrepareResult, error) {
	if a == nil || a.factory == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if a.factory.deps.MemoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := ""
	if a.factory.deps.Workspace != nil {
		workspaceSlug = memorymodule.WorkspaceSlug(a.factory.deps.Workspace.Root())
	}
	result, err := a.factory.deps.MemoryModule.Prepare(ctx, memorymodule.PrepareRequest{
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		WorkspaceSlug: workspaceSlug,
		UserInput:     req.Input,
		Mode:          string(req.OrchestrationMode),
	})
	if err != nil {
		return nil, fmt.Errorf("prepare memory: %w", err)
	}
	if err := emitMemoryPreparedEvent(ctx, a.factory.deps.Store, req, memorymodule.WorkspaceScope(workspaceSlug), result); err != nil {
		return nil, err
	}
	if result != nil {
		if err := emitProcedureActivationEvents(ctx, a.factory.deps.Store, req.Sink, req.RunID, result.ProcedureActivations); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (a *contextSelectionAssembler) ResolveSelection(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	chatModel einomodel.BaseChatModel,
) (*runSelection, error) {
	if a == nil || a.factory == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return a.factory.resolveRunSelection(ctx, req, caps, chatModel)
}

func (a *contextSelectionAssembler) AssembleToolContext(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	selection *runSelection,
	memoryPrepared *memorymodule.PrepareResult,
) (*contextplane.AssembleResult, error) {
	if a == nil || a.factory == nil || a.factory.deps.ContextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if caps == nil {
		return nil, errors.New("run capabilities are required")
	}
	if selection == nil {
		selection = &runSelection{}
	}
	result, err := a.factory.deps.ContextPlane.Assemble(ctx, contextplane.AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SelectedSkill:  selection.selectedSkill,
		SkillSnapshot:  caps.skillSnapshot,
		DecisionRecord: selection.decisionRecord,
		Hint:           selection.hint,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    caps.catalog,
	})
	if err != nil {
		return nil, err
	}
	if err := emitProcedureActivationEvents(
		ctx,
		a.factory.deps.Store,
		req.Sink,
		req.RunID,
		filterProcedureActivationsByPhase(result.ProcedureActivations, memorymodule.ProcedureActivationInjected),
	); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *contextSelectionAssembler) AssembleDirectContext(
	ctx context.Context,
	req RunnerBuildRequest,
	memoryPrepared *memorymodule.PrepareResult,
	skillSnapshot *skills.Snapshot,
	catalog *tooling.Catalog,
) (*contextplane.AssembleResult, error) {
	if a == nil || a.factory == nil || a.factory.deps.ContextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	result, err := a.factory.deps.ContextPlane.Assemble(ctx, contextplane.AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SkillSnapshot:  skillSnapshot,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    catalog,
	})
	if err != nil {
		return nil, err
	}
	if err := emitProcedureActivationEvents(
		ctx,
		a.factory.deps.Store,
		req.Sink,
		req.RunID,
		filterProcedureActivationsByPhase(result.ProcedureActivations, memorymodule.ProcedureActivationInjected),
	); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *modelProviderAssembler) SetBuildRunForTest(fn func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)) {
	a.buildRun = fn
}

type defaultOrchestrationPlaneDeps struct {
	cfg          *config.Config
	store        RunnerFactoryStore
	contextPlane contextplane.ContextPlane
	handlers     []adk.ChatModelAgentMiddleware
}

func newDefaultOrchestrationPlane(deps defaultOrchestrationPlaneDeps) *orchestration.DefaultPlane {
	toolSchemaCache := NewToolSchemaCache()
	return orchestration.NewDefaultPlane(orchestration.DefaultPlaneOptions{
		SystemPrompt:             deps.cfg.Agent.SystemPrompt,
		MaxIterations:            deps.cfg.Agent.MaxIterations,
		CheckpointStore:          deps.store,
		PlanStore:                NewPlanStore(deps.store),
		ToolBuilder:              deps.buildAuditedTools,
		ToolNodeFactory:          deps.buildToolNode,
		GraphBuilder:             BuildRuntimeAgentGraph,
		PlanExecuteGraphBuilder:  BuildRuntimePlanExecuteGraph,
		HandlersBuilder:          deps.buildHandlers,
		InstructionBuilder:       buildStableInstruction,
		ToolSchemaChangeDetector: toolSchemaCache.AnyChanged,
		ToolLifecycleBinder:      deps.bindToolLifecycle,
		StoreContextBinder:       deps.bindStore,
		SessionContextBinder:     bindSessionID,
	})
}

func (d defaultOrchestrationPlaneDeps) buildAuditedTools(
	ctx context.Context,
	specs []tooling.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	runID string,
) ([]einotool.BaseTool, error) {
	return buildAuditedTools(
		ctx,
		d.store,
		specs,
		excludedToolNames,
		allowedToolNames,
		runID,
	)
}

func (d defaultOrchestrationPlaneDeps) buildToolNode(
	ctx context.Context,
	tools []einotool.BaseTool,
	resolver tooling.ExecutionPolicyResolver,
) (orchestration.ToolInvoker, error) {
	return NewSafeParallelToolsNode(ctx, tools, resolver)
}

func BuildRuntimeAgentGraph(ctx context.Context, req orchestration.GraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, typedPromptProvider, err := runtimeGraphDependencies(req.PlanStore, req.PlanningPromptProvider)
	if err != nil {
		return nil, err
	}
	runnable, err := BuildAgentGraph(
		ctx,
		req.AgentName,
		req.ChatModel,
		req.SafeToolNode,
		req.AssistantStreamer,
		req.MaxIterations,
		req.CheckpointStore,
		req.Handlers,
		typedPlanStore,
		req.PlanPrompt,
		typedPromptProvider,
		req.EagerToolNames,
		req.ToolSpecs,
	)
	if err != nil {
		return nil, err
	}
	return graph.NewGraphAgent(
		req.AgentName,
		req.AgentDescription,
		runnable,
		req.ChatModel,
		req.Tools,
		req.Handlers,
		req.MaxIterations,
		req.CheckpointStore,
		graph.GraphAgentContextBinder(ctx),
	), nil
}

func BuildRuntimePlanExecuteGraph(ctx context.Context, req orchestration.PlanExecuteGraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, typedPromptProvider, err := runtimeGraphDependencies(req.PlanStore, req.PlanningPromptProvider)
	if err != nil {
		return nil, err
	}
	runnable, err := BuildPlanExecuteGraph(
		ctx,
		req.AgentName,
		req.ChatModel,
		req.MaxIterations,
		req.CheckpointStore,
		req.Handlers,
		typedPlanStore,
		req.PlanPrompt,
		typedPromptProvider,
		req.EagerToolNames,
		req.ToolSpecs,
		req.ChildExecutor,
	)
	if err != nil {
		return nil, err
	}
	return graph.NewGraphAgent(
		req.AgentName,
		req.AgentDescription,
		runnable,
		req.ChatModel,
		req.Tools,
		req.Handlers,
		req.MaxIterations,
		req.CheckpointStore,
		graph.GraphAgentContextBinder(ctx),
	), nil
}

func runtimeGraphDependencies(
	planStore orchestration.PlanStore,
	promptProvider orchestration.PlanningPromptProvider,
) (PlanStore, PlanningPromptProvider, error) {
	typedPlanStore, ok := planStore.(PlanStore)
	if !ok {
		return nil, nil, fmt.Errorf("orchestration plane requires runtime plan store")
	}
	typedPromptProvider, ok := promptProvider.(PlanningPromptProvider)
	if promptProvider != nil && !ok {
		return nil, nil, fmt.Errorf("orchestration plane requires runtime planning prompt provider")
	}
	return typedPlanStore, typedPromptProvider, nil
}

func (d defaultOrchestrationPlaneDeps) buildHandlers(
	ctx context.Context,
	chatModel einomodel.BaseChatModel,
	compressionState any,
) ([]adk.ChatModelAgentMiddleware, error) {
	return buildRunnerAgentHandlers(
		ctx,
		d.cfg,
		d.contextPlane,
		d.handlers,
		d.store,
		chatModel,
		compressionState,
	)
}

func buildRunnerAgentHandlers(
	ctx context.Context,
	cfg *config.Config,
	contextPlane contextplane.ContextPlane,
	extraHandlers []adk.ChatModelAgentMiddleware,
	store EventAppender,
	chatModel einomodel.BaseChatModel,
	compressionState any,
) ([]adk.ChatModelAgentMiddleware, error) {
	if cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if contextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	contextPolicy, err := cfg.ContextPolicy()
	if err != nil {
		return nil, fmt.Errorf("context policy: %w", err)
	}
	compressionHandlers, err := contextPlane.BuildHandlers(ctx, contextPolicy, chatModel, contextplane.CompressionBuildOptions{
		RuntimeStorageDir: cfg.Runtime.StorageDir,
		State:             compressionState,
		EmitCompressed: func(ctx context.Context, outcome contextplane.CompressionOutcome) error {
			return EmitContextCompressedEvent(ctx, store, outcome)
		},
		EmitPressure: func(ctx context.Context, pressure contextplane.BudgetPressure) error {
			return EmitContextPressureEvent(ctx, store, pressure)
		},
	})
	if err != nil {
		return nil, err
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, len(compressionHandlers)+len(extraHandlers)+2)
	handlers = append(handlers, compressionHandlers...)
	handlers = append(handlers, extraHandlers...)
	return handlers, nil
}

func (d defaultOrchestrationPlaneDeps) bindToolLifecycle(
	ctx context.Context,
	state orchestration.ToolLifecycleStateView,
	catalog *tooling.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	if adapter, ok := state.(toolLifecycleStateAdapter); ok && adapter.state != nil {
		return contextplane.WithToolLifecycleContext(ctx, d.contextPlane, adapter.state, catalog, infos)
	}
	return ctx
}

func (d defaultOrchestrationPlaneDeps) bindStore(ctx context.Context) context.Context {
	return WithStore(ctx, d.store)
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return WithSessionID(ctx, sessionID)
}

type toolLifecycleStateAdapter struct {
	state *contextplane.ToolLifecycleState
}

func (a toolLifecycleStateAdapter) IsLoaded(toolName string) bool {
	if a.state == nil {
		return false
	}
	_, ok := a.state.LoadedTools[toolName]
	return ok
}

func AssembleResultToView(result *contextplane.AssembleResult) orchestration.AssembleResultView {
	if result == nil {
		return orchestration.AssembleResultView{}
	}
	return orchestration.AssembleResultView{
		Messages:          result.Messages,
		LifecycleState:    toolLifecycleStateAdapter{state: result.LifecycleState},
		EagerToolNames:    result.EagerToolNames,
		DeferredToolNames: result.DeferredToolNames,
	}
}

const systemHotReloadRunID = "_system_hot_reload"

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

func hasEnabledProviders(cfgs []mcpprovider.ProviderConfig) bool {
	for _, cfg := range cfgs {
		if cfg.Enabled {
			return true
		}
	}
	return false
}

func (f *RunnerFactory) bootstrapRunMCP(ctx context.Context, req RunnerBuildRequest) (*mcpprovider.Manager, error) {
	providerConfigs := mcpprovider.ProviderConfigsFromConfig(f.deps.Config.MCP.Providers)
	if !hasEnabledProviders(providerConfigs) {
		return nil, nil
	}

	sessionOverlay := ""
	if strings.TrimSpace(req.SessionID) != "" {
		sessionOverlay = req.SessionID
	}
	manager, err := f.getOrCreateMCPManager(ctx, providerConfigs, sessionOverlay)
	if err != nil {
		return nil, err
	}
	manager.SetActiveRunID(req.RunID)
	if err := emitProviderDegradedIfNeeded(ctx, f.deps.Store, req, manager.Statuses()); err != nil {
		return nil, err
	}
	return manager, nil
}

func (f *RunnerFactory) getOrCreateMCPManager(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig, sessionOverlay string) (*mcpprovider.Manager, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cachedManager == nil {
		pendingActionStore := mcpprovider.PendingActionStore(f.deps.Store)
		if f.deps.MCPPendingActions != nil {
			pendingActionStore = f.deps.MCPPendingActions
		}
		mgr, err := mcpprovider.NewManager(ctx, providerConfigs, mcpprovider.WithEventCallback(f.providerEventCallback()), mcpprovider.WithTokenStore(f.deps.Store), mcpprovider.WithStore(pendingActionStore))
		if err != nil {
			return nil, err
		}
		f.cachedManager = mgr
		f.lastSessionOverlay = sessionOverlay

		childExec := f.newChildAgentExecutor()
		mgr.SetSamplingExecutor(subagentExecutorAdapter{exec: childExec})

		return mgr, nil
	}

	if f.lastSessionOverlay != sessionOverlay {
		if err := f.cachedManager.ReconcileProviders(ctx, providerConfigs); err != nil {
			return nil, fmt.Errorf("reconcile MCP providers for new session overlay: %w", err)
		}
		f.lastSessionOverlay = sessionOverlay
	}

	return f.cachedManager, nil
}

func (f *RunnerFactory) providerEventCallback() mcpprovider.ProviderEventCallback {
	return func(ev mcpprovider.ProviderEvent) {
		runID := f.currentRunIDValue()

		var sink StreamSink
		if rc, ok := f.registry.Get(runID); ok {
			sink = rc.Sink
		}

		if strings.TrimSpace(runID) == "" {
			runID = systemHotReloadRunID
			sink = nil
		}

		var kind StreamItemKind
		switch ev.Kind {
		case "tool_catalog_refreshed":
			kind = StreamKindMCPToolCatalogRefreshed
		case "tool_catalog_refresh_failed":
			kind = StreamKindMCPToolCatalogRefreshFailed
		case "provider_added":
			kind = StreamKindMCPProviderAdded
		case "provider_removed":
			kind = StreamKindMCPProviderRemoved
		case "provider_restarted":
			kind = StreamKindMCPProviderRestarted
		case "resource_catalog_refreshed":
			kind = StreamKindMCPResourceCatalogRefreshed
		case "resource_catalog_refresh_failed":
			kind = StreamKindMCPResourceCatalogRefreshFailed
		case "prompt_catalog_refreshed":
			kind = StreamKindMCPPromptCatalogRefreshed
		case "prompt_catalog_refresh_failed":
			kind = StreamKindMCPPromptCatalogRefreshFailed
		case "auth_status_changed":
			kind = StreamKindMCPAuthStatusChanged
		default:
			f.recordEventError(runID, fmt.Errorf("unknown MCP provider event %q", ev.Kind))
			return
		}

		payload := MCPProviderLifecyclePayload{
			ProviderName: ev.Provider,
			Transport:    ev.Transport,
			Error:        ev.Error,
			AuthStatus:   ev.AuthStatus,
		}

		if _, err := AppendStreamItem(context.Background(), f.deps.Store, sink, StreamItem{
			RunID:     runID,
			Kind:      kind,
			CreatedAt: time.Now().UTC(),
			Payload:   &payload,
		}); err != nil {
			f.recordEventError(runID, fmt.Errorf("append MCP lifecycle stream item %s: %w", kind, err))
		}
	}
}

func (f *RunnerFactory) recordEventError(runID string, err error) {
	if f == nil || err == nil || strings.TrimSpace(runID) == "" {
		return
	}
	f.eventMu.Lock()
	defer f.eventMu.Unlock()
	if f.eventErrors == nil {
		f.eventErrors = make(map[string]error)
	}
	f.eventErrors[runID] = errors.Join(f.eventErrors[runID], err)
}

func (f *RunnerFactory) consumeEventError(runID string) error {
	if f == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	f.eventMu.Lock()
	defer f.eventMu.Unlock()
	if len(f.eventErrors) == 0 {
		return nil
	}
	err := f.eventErrors[runID]
	delete(f.eventErrors, runID)
	return err
}

func (f *RunnerFactory) ReconcileMCPProviders(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig) error {
	f.mu.Lock()
	mgr := f.cachedManager
	f.mu.Unlock()

	if mgr == nil {
		return nil
	}

	if err := mgr.ReconcileProviders(ctx, providerConfigs); err != nil {
		return err
	}
	if err := f.consumeEventError(systemHotReloadRunID); err != nil {
		return err
	}
	return nil
}

func (f *RunnerFactory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var closeErr error
	if f.cachedManager != nil {
		closeErr = errors.Join(closeErr, f.cachedManager.Close())
		f.cachedManager = nil
		f.lastSessionOverlay = ""
	}
	if f.deps.IndexStore != nil {
		closeErr = errors.Join(closeErr, f.deps.IndexStore.Close())
		f.deps.IndexStore = nil
		f.deps.Crystallizer = nil
	}
	return closeErr
}

type Toolset struct {
	catalog *tooling.Catalog
	profile tooling.ToolProfile
	closers []toolsetCloser
}

type toolsetCloser interface {
	Close() error
}

func (t Toolset) All() []einotool.BaseTool {
	if t.catalog == nil {
		return nil
	}
	return t.catalog.ToolsForProfile(t.profile)
}

func (t Toolset) Catalog() *tooling.Catalog {
	return t.catalog
}

func (t *Toolset) Close() error {
	if t == nil {
		return nil
	}
	var errs []error
	for i := len(t.closers) - 1; i >= 0; i-- {
		if t.closers[i] == nil {
			continue
		}
		if err := t.closers[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f *RunnerFactory) BuildServeToolset(ctx context.Context) (*Toolset, error) {
	return f.buildToolset(ctx, "", nil, false, tooling.ToolProfileServe)
}

type delegateTaskBridge struct{}

func (delegateTaskBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (delegateTaskBridge) CurrentSessionID(ctx context.Context) string {
	return SessionIDFromContext(ctx)
}

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return SessionIDFromContext(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return toolAuditCallID(ctx)
}

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string, childExec orchestration.ChildAgentExecutor) (*Toolset, error) {
	return f.buildToolset(ctx, sessionID, childExec, true, tooling.ToolProfileRun)
}

func (f *RunnerFactory) buildToolset(
	ctx context.Context,
	sessionID string,
	childExec orchestration.ChildAgentExecutor,
	includePlanning bool,
	profile tooling.ToolProfile,
) (*Toolset, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.deps.Workspace == nil {
		return nil, errors.New("workspace contract is not initialized")
	}
	if f.deps.ArtifactService == nil {
		return nil, errors.New("artifact service is not initialized")
	}
	if f.deps.TerminalService == nil {
		return nil, errors.New("terminal session service is not initialized")
	}

	webFetchService, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        f.deps.Config.WebAccess.UserAgent,
		Timeout:          time.Duration(f.deps.Config.WebAccess.TimeoutSeconds) * time.Second,
		MaxResponseBytes: f.deps.Config.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web fetch service: %w", err)
	}
	webSearchService, err := webaccess.NewSearchService(webaccess.SearchConfig{
		APIKey:           f.deps.Config.WebAccess.Search.APIKey,
		Timeout:          time.Duration(f.deps.Config.WebAccess.Search.TimeoutSeconds) * time.Second,
		MaxResults:       f.deps.Config.WebAccess.Search.MaxResults,
		MaxResponseBytes: f.deps.Config.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web search service: %w", err)
	}
	browserService, err := browser.NewService(browser.Config{
		ExecutablePath: strings.TrimSpace(f.deps.Config.Browser.ExecutablePath),
		Headless:       f.deps.Config.Browser.Headless,
		Timeout:        time.Duration(f.deps.Config.Browser.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      f.deps.Config.WebAccess.UserAgent,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("browser service: %w", err)
	}

	var operatorStore tools.OperatorQuestionStore
	if f.deps.MCPPendingActions != nil {
		operatorStore = f.deps.MCPPendingActions
	} else if store, ok := f.deps.Store.(tools.OperatorQuestionStore); ok {
		operatorStore = store
	}

	localCatalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         f.deps.Workspace,
		MutationEnabled:   !f.deps.Config.Tools.Mutation.Disabled,
		RunCommandEnabled: !f.deps.Config.Tools.RunCommand.Disabled,
		ArtifactService:   f.deps.ArtifactService,
		ArtifactContext:   artifactToolBridge{},
		TerminalService:   f.deps.TerminalService,
		TerminalContext:   artifactToolBridge{},
		OperatorStore:     operatorStore,
		OperatorContext:   artifactToolBridge{},
		WebFetchService:   webFetchService,
		WebSearchService:  webSearchService,
		BrowserService:    browserService,
	}, f.deps.ExtraLocalTools, childExec, delegateTaskBridge{})
	if err != nil {
		return nil, err
	}

	checkpointService := f.deps.CheckpointService
	effectiveSessionID := sessionID
	if !includePlanning {
		checkpointService = nil
		effectiveSessionID = ""
	}
	checkpointTools, err := workingstate.BuildWorkingCheckpointTools(checkpointService, effectiveSessionID)
	if err != nil {
		return nil, fmt.Errorf("build working checkpoint tools: %w", err)
	}
	var memoryTools []einotool.BaseTool
	if f.deps.MemoryModule != nil {
		fileTools, err := buildMemoryFileTools(ctx, f.deps.MemoryModule)
		if err != nil {
			return nil, err
		}
		memoryTools = append(memoryTools, fileTools...)
	}

	skillTools, err := skills.BuildAgentTools(f.deps.Loader)
	if err != nil {
		return nil, fmt.Errorf("build skill tools: %w", err)
	}
	var skillLifecycleTools []einotool.BaseTool
	if includePlanning {
		skillLifecycleTools, err = skilllifecycle.BuildAgentTools(skilllifecycle.ToolOptions{
			Loader: f.deps.Loader,
			Store:  f.deps.Store,
			Bridge: delegateTaskBridge{},
		})
		if err != nil {
			return nil, fmt.Errorf("build skill lifecycle tools: %w", err)
		}
	}

	specs, err := buildCatalogSpecs(ctx, f.deps.Config, "local", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	checkpointSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "workingstate", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, checkpointTools)
	if err != nil {
		return nil, err
	}
	memorySpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "memory", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, memoryTools)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "skill", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, skillTools)
	if err != nil {
		return nil, err
	}
	skillLifecycleSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "skill.lifecycle", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun}, skillLifecycleTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, checkpointSpecs...)
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	specs = append(specs, skillLifecycleSpecs...)

	if includePlanning {
		loadToolsTool, err := newLoadToolsTool(f.deps.ContextPlane)
		if err != nil {
			return nil, fmt.Errorf("build load_tools tool: %w", err)
		}
		planningSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "runtime", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, []einotool.BaseTool{loadToolsTool})
		if err != nil {
			return nil, err
		}
		specs = append(specs, planningSpecs...)
	}
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return &Toolset{catalog: catalog, profile: profile, closers: []toolsetCloser{browserService}}, nil
}

const capabilityDiscoveryInstruction = `Capability discovery rules:
- Before answering a capability question or saying you cannot do something, inspect the skill catalog and currently loaded tools already present in context.
- If a relevant skill may exist but the catalog summary is not enough, call skill_list or skill_view before answering.
- If a relevant capability depends on deferred tools, call load_tools before concluding the capability is unavailable.
- Prefer the matching skill and tool path over a generic limitation answer.`

func emitProviderDegradedIfNeeded(ctx context.Context, store EventAppender, req RunnerBuildRequest, statuses []mcpprovider.ProviderStatus) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	var healthy, failed bool
	var failedEntries []ProviderDegradedEntry
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		if s.StartupStatus == "healthy" {
			healthy = true
		} else if s.StartupStatus == "failed" {
			failed = true
			failedEntries = append(failedEntries, ProviderDegradedEntry{
				Name:      s.Name,
				Transport: s.Transport,
				Error:     s.Error,
			})
		}
	}
	if !healthy || !failed {
		return nil
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindProviderDegraded,
		CreatedAt: time.Now().UTC(),
		Payload: &ProviderDegradedPayload{
			AffectedProviders: failedEntries,
		},
	})
	return err
}

func emitMemoryPreparedEvent(ctx context.Context, store EventAppender, req RunnerBuildRequest, workspaceScope string, result *memorymodule.PrepareResult) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	prepared := &StreamMemoryPrepared{
		Query:          strings.TrimSpace(req.Input),
		WorkspaceScope: strings.TrimSpace(workspaceScope),
	}
	if result != nil {
		prepared.NudgeCount = len(result.Nudges)
		prepared.EntryCount = len(result.Entries)
		prepared.Nudges = make([]StreamMemoryPreparedNudge, 0, len(result.Nudges))
		for _, nudge := range result.Nudges {
			prepared.Nudges = append(prepared.Nudges, StreamMemoryPreparedNudge{
				Ref:    nudge.Ref,
				Kind:   nudge.Kind,
				Title:  nudge.Title,
				Status: nudge.Status,
				Reason: nudge.Reason,
			})
		}
		prepared.Entries = make([]StreamMemoryPreparedEntry, 0, len(result.Entries))
		for _, entry := range result.Entries {
			prepared.Entries = append(prepared.Entries, StreamMemoryPreparedEntry{
				Ref:   entry.Ref,
				Kind:  entry.Kind,
				Title: entry.Title,
			})
		}
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindMemoryPrepared,
		CreatedAt: time.Now().UTC(),
		Payload:   &MemoryPreparedPayload{MemoryPrepared: prepared},
	})
	return err
}

func emitProcedureActivationEvents(ctx context.Context, store EventAppender, sink StreamSink, runID string, activations []memorymodule.ProcedureActivation) error {
	if store == nil || strings.TrimSpace(runID) == "" || len(activations) == 0 {
		return nil
	}
	for _, activation := range activations {
		_, err := AppendStreamItem(ctx, store, sink, StreamItem{
			RunID:     runID,
			Kind:      StreamKindProcedureActivation,
			CreatedAt: time.Now().UTC(),
			Payload: &ProcedureActivationPayload{
				ProcedureActivation: streamProcedureActivationFromDomain(runID, activation),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func filterProcedureActivationsByPhase(items []memorymodule.ProcedureActivation, phase memorymodule.ProcedureActivationPhase) []memorymodule.ProcedureActivation {
	if len(items) == 0 {
		return nil
	}
	out := make([]memorymodule.ProcedureActivation, 0, len(items))
	for _, item := range items {
		if item.Phase == phase {
			out = append(out, item)
		}
	}
	return out
}

func streamProcedureActivationFromDomain(runID string, item memorymodule.ProcedureActivation) *StreamProcedureActivation {
	effectiveRunID := strings.TrimSpace(item.RunID)
	if effectiveRunID == "" {
		effectiveRunID = strings.TrimSpace(runID)
	}
	return &StreamProcedureActivation{
		RunID:        effectiveRunID,
		SessionID:    strings.TrimSpace(item.SessionID),
		ProcedureRef: strings.TrimSpace(item.ProcedureRef),
		Title:        strings.TrimSpace(item.Title),
		Kind:         strings.TrimSpace(item.Kind),
		Phase:        string(item.Phase),
		Reason:       strings.TrimSpace(item.Reason),
		Score:        item.Score,
		Status:       string(item.Status),
		Origin:       string(item.Origin),
		SourceRefs:   append([]string(nil), item.SourceRefs...),
		EvidenceRefs: append([]string(nil), item.EvidenceRefs...),
	}
}

func buildStableInstruction(base string, instructionSuffix string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(capabilityDiscoveryInstruction),
		strings.TrimSpace(instructionSuffix),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}

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
		newModel = appmodel.NewChatModel
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

func (f *RunnerFactory) newChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRuntimeChatModel(ctx, f.deps.Config, nil, nil)
}

// Subagent execution is now handled by SubagentExecutor in subagent_executor.go.
// The samplingExecutorAdapter has been replaced.

func skillEligibilityContextFromCatalog(catalog *tooling.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tooling.EligibilityContextForProfile(catalog, tooling.ToolProfileRun, nil)
}

func emitSkillSelectionEvents(ctx context.Context, store EventAppender, req RunnerBuildRequest, selected *SelectedSkill, matches []SkillMatch) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	candidates := topSkillCandidates(matches, 3)
	discoveredSkill := &StreamSkill{
		Candidates: candidates,
	}
	if selected == nil {
		discoveredSkill.NoSelectionReason = deriveNoSelectionReason(req, matches)
	}
	if _, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillDiscovered,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillDiscoveredPayload{Skill: discoveredSkill},
	}); err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	streamSkill := streamSkillFromSelected(selected, candidates)
	if _, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillSelected,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillSelectedPayload{Skill: streamSkill},
	}); err != nil {
		return err
	}
	if _, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillLoaded,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillLoadedPayload{Skill: streamSkill},
	}); err != nil {
		return err
	}
	if err := emitProcedureActivationEvents(ctx, store, req.Sink, req.RunID, []memorymodule.ProcedureActivation{
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationSelected, "decision_selected_skill"),
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationUsed, "skill_loaded_for_run"),
	}); err != nil {
		return err
	}
	return nil
}

func deriveNoSelectionReason(req RunnerBuildRequest, matches []SkillMatch) string {
	if strings.TrimSpace(req.SkillID) != "" {
		if len(matches) == 0 {
			return "explicit_skill_missing"
		}
		if matches[0].FilteredReason != "" {
			return "explicit_skill_ineligible"
		}
	}
	eligible := make([]SkillMatch, 0, len(matches))
	for _, item := range matches {
		if item.FilteredReason != "" || item.Score <= 0 || !item.TriggerMatched {
			continue
		}
		eligible = append(eligible, item)
	}
	if len(eligible) == 0 {
		return noEligibleSkillMatchReason
	}
	if len(eligible) > 1 && eligible[0].Score == eligible[1].Score {
		return ambiguousTopScoreReason
	}
	return ""
}

func topSkillCandidates(matches []SkillMatch, limit int) []StreamSkillCandidate {
	if limit <= 0 || len(matches) == 0 {
		return nil
	}
	if len(matches) < limit {
		limit = len(matches)
	}
	items := make([]StreamSkillCandidate, 0, limit)
	for _, item := range matches[:limit] {
		items = append(items, StreamSkillCandidate{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			FilteredReason: item.FilteredReason,
			Requirements:   StreamSkillRequirementsFromDomain(item.Skill.Requires),
			Summary:        item.Skill.Summary,
			Origin:         string(item.Skill.Origin),
			TaskPattern:    item.Skill.TaskPattern,
		})
	}
	return items
}

func streamSkillFromSelected(selected *SelectedSkill, candidates []StreamSkillCandidate) *StreamSkill {
	if selected == nil {
		return nil
	}
	return &StreamSkill{
		SelectedID:   selected.Skill.ID,
		Name:         selected.Skill.Name,
		Candidates:   candidates,
		Source:       selected.Skill.Source,
		Origin:       string(selected.Skill.Origin),
		TaskPattern:  selected.Skill.TaskPattern,
		Path:         selected.Skill.Path,
		Summary:      selected.Skill.Summary,
		Instruction:  selected.Skill.Instruction,
		Scripts:      append([]string(nil), selected.Skill.Scripts...),
		Requirements: StreamSkillRequirementsFromDomain(selected.Skill.Requires),
		Score:        selected.Score,
		MatchedTerms: append([]string(nil), selected.MatchedTerms...),
	}
}

func procedureActivationFromSelectedSkill(req RunnerBuildRequest, selected *SelectedSkill, phase memorymodule.ProcedureActivationPhase, reason string) memorymodule.ProcedureActivation {
	if selected == nil {
		return memorymodule.ProcedureActivation{}
	}
	return memorymodule.ProcedureActivation{
		RunID:        strings.TrimSpace(req.RunID),
		SessionID:    strings.TrimSpace(req.SessionID),
		ProcedureRef: strings.TrimSpace(selected.Skill.ID),
		Title:        strings.TrimSpace(selected.Skill.Name),
		Kind:         "executable_skill",
		Phase:        phase,
		Reason:       reason,
		Score:        float64(selected.Score),
		Origin:       memorymodule.ProcedureOrigin(strings.TrimSpace(string(selected.Skill.Origin))),
		SourceRefs:   nonEmptyStrings(selected.Skill.PromotedFrom, selected.Skill.Path),
	}
}

func nonEmptyStrings(items ...string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func loadStableSkillSnapshot(ctx context.Context, loader interface {
	ScanSkills(context.Context) (*skills.ScanResult, error)
}, eligibility skills.EligibilityContext) (*skills.Snapshot, error) {
	if loader == nil {
		return nil, nil
	}
	scan, err := loader.ScanSkills(ctx)
	if err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}
	if scan == nil {
		return nil, nil
	}
	snapshot, err := skills.BuildSnapshot(*scan, eligibility)
	if err != nil {
		return nil, fmt.Errorf("build skill snapshot: %w", err)
	}
	copied := skills.CopySnapshot(snapshot)
	return &copied, nil
}

func stableSkillsFromSnapshot(snapshot *skills.Snapshot) []skills.Spec {
	if snapshot == nil || len(snapshot.Skills) == 0 {
		return nil
	}
	items := make([]skills.Spec, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		items = append(items, skills.CopySpec(item.Spec))
	}
	return items
}

func recommendedSkillsFromMatches(matches []SkillMatch) []decision.RecommendedSkill {
	items := make([]decision.RecommendedSkill, 0, len(matches))
	for _, item := range matches {
		items = append(items, decision.RecommendedSkill{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			TriggerMatched: item.TriggerMatched,
			FilteredReason: item.FilteredReason,
		})
	}
	return items
}

func emitDecisionEvents(ctx context.Context, store EventAppender, req RunnerBuildRequest, record *decision.Record, explicitSkillID string) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" || record == nil {
		return nil
	}
	finalKind := StreamKindDecisionSelected
	if record.Action == decision.ActionAskUser || record.Action == decision.ActionBlock || record.Action == decision.ActionResumeRun {
		finalKind = StreamKindDecisionBlocked
	}
	decisionPayload := &DecisionSelectedPayload{
		Action:              string(record.Action),
		Intent:              record.Intent,
		SelectedSkillID:     record.SelectedSkillID,
		DecisionReason:      record.DecisionReason,
		DecisionProfileHash: record.DecisionProfileHash,
		ExplicitSkillID:     strings.TrimSpace(explicitSkillID),
	}
	if finalKind == StreamKindDecisionBlocked {
		_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
			RunID:     req.RunID,
			Kind:      finalKind,
			CreatedAt: time.Now().UTC(),
			Payload: &DecisionBlockedPayload{
				Action:              string(record.Action),
				Intent:              record.Intent,
				SelectedSkillID:     record.SelectedSkillID,
				DecisionReason:      record.DecisionReason,
				DecisionProfileHash: record.DecisionProfileHash,
				ExplicitSkillID:     strings.TrimSpace(explicitSkillID),
			},
		})
		return err
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      finalKind,
		CreatedAt: time.Now().UTC(),
		Payload:   decisionPayload,
	})
	return err
}

func StreamSkillRequirementsFromDomain(item skills.Requirements) StreamSkillRequirements {
	return StreamSkillRequirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return value
		}
	}
	return ""
}

func runtimeMatchesFromRecommendations(items []skills.Recommendation) []SkillMatch {
	if len(items) == 0 {
		return nil
	}
	out := make([]SkillMatch, 0, len(items))
	for _, item := range items {
		out = append(out, SkillMatch{
			Skill:          skills.CopySpec(item.Skill),
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			TriggerMatched: item.TriggerMatched,
			FilteredReason: item.FilteredReason,
		})
	}
	return out
}
