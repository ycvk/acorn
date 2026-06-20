package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
)

// buildAssembly is the single assembly entry shared by all orchestration modes.
// It dispatches to the mode-specific orchestration.Build* call, reusing the
// common baseAssemblyFields helper so agent/session/tool fields are not
// duplicated across the three request constructors.
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
	switch mode {
	case events.ModeDirectResponse:
		return f.deps.Orchestration.BuildDirectResponse(ctx, f.directResponseRequest(bf, req))
	case events.ModeSingleAgent:
		return f.buildSingleAgentRun(ctx, bf, req)
	case events.ModePlanExecute:
		return f.buildPlanExecuteRun(ctx, bf, req)
	default:
		return nil, fmt.Errorf("unsupported orchestration mode %q", mode)
	}
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

func (f *RunnerFactory) buildSingleAgentRun(ctx context.Context, bf baseAssemblyFields, req RunnerBuildRequest) (*orchestration.RunAssembly, error) {
	instructionSuffix, err := f.withMemoryInstruction(ctx, req)
	if err != nil {
		return nil, err
	}
	return f.deps.Orchestration.BuildSingleAgent(ctx, orchestration.SingleAgentRequest{
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
		InstructionSuffix: instructionSuffix,
	})
}

func (f *RunnerFactory) buildPlanExecuteRun(ctx context.Context, bf baseAssemblyFields, req RunnerBuildRequest) (*orchestration.RunAssembly, error) {
	instructionSuffix, err := f.withMemoryInstruction(ctx, req)
	if err != nil {
		return nil, err
	}
	childExec, err := f.newChildAgentExecutor()
	if err != nil {
		return nil, err
	}
	return f.deps.Orchestration.BuildPlanExecute(ctx, orchestration.PlanExecuteRequest{
		AgentName:         bf.agentName,
		AgentDescription:  bf.agentDescription,
		SessionID:         bf.sessionID,
		RunID:             bf.runID,
		ChatModel:         bf.chatModel,
		Catalog:           bf.catalog,
		ContextResult:     bf.contextResult,
		AllowedToolNames:  bf.allowedToolNames,
		ExcludedToolNames: bf.excludedToolNames,
		InstructionSuffix: instructionSuffix,
		ChildExecutor:     childExec,
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
