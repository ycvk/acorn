package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

type runCapabilities struct {
	catalog      *tooling.Catalog
	stableSkills []skills.Spec
	close        func() error
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
	stableSkills, err := loadStableSkills(ctx, f.loader)
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
		spec, err := runtimeToolSpec(ctx, f.cfg, registration.ProviderName, tooling.ToolKindMCP, []tooling.ToolProfile{tooling.ToolProfileRun}, namespaced)
		if err != nil {
			return nil, err
		}
		parallelPolicy, err := mcpToolParallelPolicy(f.cfg, registration.ProviderName)
		if err != nil {
			return nil, fmt.Errorf("resolve MCP tool safety for provider %q: %w", registration.ProviderName, err)
		}
		spec.Execution.ParallelPolicy = parallelPolicy
		specs = append(specs, spec)
	}
	resourceSpecs, err := buildCatalogSpecs(ctx, f.cfg, "mcp.resource", tooling.ToolKindMCPResource, []tooling.ToolProfile{tooling.ToolProfileRun}, resourceTools)
	if err != nil {
		return nil, err
	}
	promptSpecs, err := buildCatalogSpecs(ctx, f.cfg, "mcp.prompt", tooling.ToolKindMCPPrompt, []tooling.ToolProfile{tooling.ToolProfileRun}, promptTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, resourceSpecs...)
	specs = append(specs, promptSpecs...)

	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, err
	}
	return &runCapabilities{
		catalog:      catalog,
		stableSkills: stableSkills,
		close:        toolset.Close,
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
		engine, parsed, err := buildPlaneEngine(f.decisionProfiles)
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
		bridge := newPlaneBridge(
			engine,
			f.decisionProfiles,
			f.store,
			func(ctx context.Context, record decision.Record) error {
				return f.store.SaveRunDecision(ctx, record)
			},
			func(ctx context.Context, record *decision.Record, explicitSkillID string) error {
				return emitDecisionEvents(ctx, f.store, req, record, explicitSkillID)
			},
		)
		planeReq := buildPlaneRequest(req, caps, discovered, hasWorkingContext)
		result, err := bridge.Decide(ctx, planeReq)
		if err != nil {
			return nil, err
		}
		fillRecordMetadata(result.Record, parsed.Hash)
		if bridge.saveFn != nil {
			if err := bridge.saveFn(ctx, *result.Record); err != nil {
				return nil, err
			}
		}
		if bridge.emitFn != nil {
			if err := bridge.emitFn(ctx, result.Record, req.SkillID); err != nil {
				return nil, err
			}
		}
		enrichSelectedSkillFromMatches(result, discovered, caps.stableSkills)
		selectedSkill, err := selectedSkillFromPlaneResult(result, caps.stableSkills)
		if err != nil {
			return nil, err
		}
		if emitErr := emitSkillSelectionEvents(ctx, f.store, req, selectedSkill, discovered); emitErr != nil {
			return nil, emitErr
		}
		if actionErr := bridge.HandleAction(ctx, decision.ActionRequest{
			RunID:     req.RunID,
			SessionID: req.SessionID,
			Input:     req.Input,
			Result:    result,
			ChatModel: chatModel,
			Skills:    skillIDs(caps.stableSkills),
		}); actionErr != nil {
			return nil, actionErr
		}
		return &runSelection{
			decisionRecord: result.Record,
			selectedSkill:  selectedSkill,
			hint:           result.Hint,
		}, nil
	} else if strings.TrimSpace(req.RunID) != "" {
		var err error
		decisionRecord, err := f.store.LoadRunDecision(ctx, req.RunID)
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
	if f == nil || f.orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	instructionSuffix, err := f.withMemoryInstruction(ctx, req)
	if err != nil {
		return nil, err
	}
	return f.orchestration.BuildSingleAgent(ctx, orchestration.SingleAgentRequest{
		AgentName:         f.cfg.Agent.Name,
		AgentDescription:  f.cfg.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		AssistantStreamer: newDirectAssistantStreamer(f.store),
		Catalog:           catalog,
		ContextResult:     contextResult,
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
	if f == nil || f.orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	return f.orchestration.BuildDirectResponse(ctx, orchestration.DirectResponseRequest{
		AgentName:         f.cfg.Agent.Name,
		AgentDescription:  f.cfg.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		AssistantStreamer: newDirectAssistantStreamer(f.store),
		Catalog:           catalog,
		ContextResult:     contextResult,
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
	if f == nil || f.orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	instructionSuffix, err := f.withMemoryInstruction(ctx, req)
	if err != nil {
		return nil, err
	}
	return f.orchestration.BuildPlanExecute(ctx, orchestration.PlanExecuteRequest{
		AgentName:         f.cfg.Agent.Name,
		AgentDescription:  f.cfg.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		Catalog:           catalog,
		ContextResult:     contextResult,
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		ExcludedToolNames: append([]string(nil), req.ExcludedToolNames...),
		InstructionSuffix: instructionSuffix,
		ChildExecutor:     f.newChildAgentExecutor(),
	})
}

func (f *RunnerFactory) withMemoryInstruction(ctx context.Context, req RunnerBuildRequest) (string, error) {
	if f == nil || f.memoryModule == nil {
		return "", errors.New("memory module is not initialized")
	}
	workspaceSlug := ""
	if f.workspace != nil {
		workspaceSlug = memorymodule.WorkspaceSlug(f.workspace.Root())
	}
	instruction, err := f.memoryModule.BuildMemoryInstruction(ctx, workspaceSlug)
	if err != nil {
		return "", fmt.Errorf("build memory instruction: %w", err)
	}
	return buildStableInstruction(req.InstructionSuffix, instruction), nil
}
