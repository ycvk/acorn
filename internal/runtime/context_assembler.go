package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/tools"
)

func prepareRunMemory(ctx context.Context, deps RuntimeDeps, req RunnerBuildRequest) (*memory.PrepareResult, error) {
	if deps.MemoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := workspaceSlug(deps)
	result, err := deps.MemoryModule.Prepare(ctx, memory.PrepareRequest{
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		WorkspaceSlug: workspaceSlug,
		UserInput:     req.Input,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare memory: %w", err)
	}
	if err := emitRunMemoryEvents(ctx, deps, req, workspaceSlug, result); err != nil {
		return nil, err
	}
	return result, nil
}

func workspaceSlug(deps RuntimeDeps) string {
	if deps.Workspace == nil {
		return ""
	}
	return memory.WorkspaceSlug(deps.Workspace.Root())
}

func emitRunMemoryEvents(ctx context.Context, deps RuntimeDeps, req RunnerBuildRequest, workspaceSlug string, result *memory.PrepareResult) error {
	if err := emitMemoryPreparedEvent(ctx, deps.Store, req, memory.WorkspaceScope(workspaceSlug), result); err != nil {
		return err
	}
	return nil
}

func assembleContext(
	ctx context.Context,
	deps RuntimeDeps,
	req RunnerBuildRequest,
	caps *runCapabilities,
	memoryPrepared *memory.PrepareResult,
) (*AssembleResult, error) {
	if deps.ContextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if caps == nil {
		return nil, errors.New("run capabilities are required")
	}
	result, err := deps.ContextPlane.Assemble(ctx, buildAssembleRequest(req, caps, memoryPrepared))
	if err != nil {
		return nil, err
	}
	return result, nil
}

// buildAssembly dispatches to the direct_response orchestration plane,
// reusing the common baseAssemblyFields helper so agent/session/tool fields
// are not duplicated across request constructors.
func buildAssembly(
	ctx context.Context,
	deps RuntimeDeps,
	req RunnerBuildRequest,
	catalog *tools.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *AssembleResult,
) (*RunAssembly, error) {
	if deps.Config == nil {
		return nil, fmt.Errorf("runner factory is not initialized")
	}
	bf := buildBaseAssemblyFields(deps, req, catalog, chatModel, contextResult)
	return buildDirectResponse(ctx, deps, directResponseRequest(deps, bf, req))
}

func buildBaseAssemblyFields(deps RuntimeDeps, req RunnerBuildRequest, catalog *tools.Catalog, chatModel einomodel.BaseChatModel, contextResult *AssembleResult) baseAssemblyFields {
	return baseAssemblyFields{
		agentName:         deps.Config.Agent.Name,
		agentDescription:  deps.Config.Agent.Description,
		sessionID:         req.SessionID,
		runID:             req.RunID,
		chatModel:         chatModel,
		catalog:           catalog,
		contextResult:     AssembleResultToView(contextResult),
		allowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		excludedToolNames: append([]string(nil), req.ExcludedToolNames...),
	}
}

func directResponseRequest(deps RuntimeDeps, bf baseAssemblyFields, req RunnerBuildRequest) DirectResponseRequest {
	return DirectResponseRequest{
		AgentName:         bf.agentName,
		AgentDescription:  bf.agentDescription,
		SessionID:         bf.sessionID,
		RunID:             bf.runID,
		ChatModel:         bf.chatModel,
		AssistantStreamer: NewDirectAssistantStreamer(deps.Store),
		Catalog:           bf.catalog,
		ContextResult:     bf.contextResult,
		AllowedToolNames:  bf.allowedToolNames,
		ExcludedToolNames: bf.excludedToolNames,
		InstructionSuffix: req.InstructionSuffix,
	}
}

type baseAssemblyFields struct {
	agentName         string
	agentDescription  string
	sessionID         string
	runID             string
	chatModel         einomodel.BaseChatModel
	catalog           *tools.Catalog
	contextResult     AssembleResultView
	allowedToolNames  []string
	excludedToolNames []string
}

func buildAssembleRequest(req RunnerBuildRequest, caps *runCapabilities, memoryPrepared *memory.PrepareResult) AssembleRequest {
	return AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SkillSnapshot:  caps.skillSnapshot,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    caps.catalog,
	}
}
