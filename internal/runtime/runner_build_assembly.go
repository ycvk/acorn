package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
)

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
		AssistantStreamer: tool.NewDirectAssistantStreamer(f.deps.Store),
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
		AssistantStreamer: tool.NewDirectAssistantStreamer(f.deps.Store),
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
	childExec, err := f.newChildAgentExecutor()
	if err != nil {
		return nil, err
	}
	return f.deps.Orchestration.BuildPlanExecute(ctx, f.buildPlanExecuteRequest(req, catalog, chatModel, contextResult, instructionSuffix, childExec))
}

func (f *RunnerFactory) buildPlanExecuteRequest(req RunnerBuildRequest, catalog *tooling.Catalog, chatModel einomodel.BaseChatModel, contextResult *contextplane.AssembleResult, instructionSuffix string, childExec orchestration.ChildAgentExecutor) orchestration.PlanExecuteRequest {
	return orchestration.PlanExecuteRequest{
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
		ChildExecutor:     childExec,
	}
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
