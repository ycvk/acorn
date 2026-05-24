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
