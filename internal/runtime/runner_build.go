package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
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
	return &RunnerFactory{
		deps:     deps,
		registry: NewRegistry(),
	}
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
) (*runSelection, error) {
	if caps == nil {
		return nil, fmt.Errorf("run capabilities are required")
	}

	if strings.TrimSpace(req.Input) == "" && strings.TrimSpace(req.SkillID) == "" && strings.TrimSpace(req.RunID) == "" {
		return &runSelection{}, nil
	}

	if strings.TrimSpace(req.Input) != "" || strings.TrimSpace(req.SkillID) != "" {
		engine, parsed, err := buildDecisionEngine(f.deps.DecisionProfiles)
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
		input := buildDecisionInput(req, discovered, hasWorkingContext)
		record, err := engine.Decide(ctx, input)
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
		selectedSkill, err := selectedSkillFromDecisionRecord(record, discovered, caps.stableSkills)
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
			decisionRecord: record,
			selectedSkill:  selectedSkill,
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
		selectedSkill, err := selectedSkillFromDecisionRecord(decisionRecord, nil, caps.stableSkills)
		if err != nil {
			return nil, err
		}
		return &runSelection{
			decisionRecord: decisionRecord,
			selectedSkill:  selectedSkill,
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
