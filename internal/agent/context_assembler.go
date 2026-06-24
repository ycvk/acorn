package agent

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	cp "github.com/ycvk/acorn/internal/context"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/tools"
)

// ContextAssembler owns run-context assembly for a RunnerFactory: memory
// preparation, context-plane assembly, and the direct-response orchestration
// request construction. It isolates context wiring from the factory so the
// factory stays a thin coordinator.
type ContextAssembler struct {
	deps          RuntimeDeps
	toolAssembler *ToolAssembler
}

// NewContextAssembler assembles a ContextAssembler from runtime deps.
func NewContextAssembler(deps RuntimeDeps) *ContextAssembler {
	return &ContextAssembler{deps: deps, toolAssembler: NewToolAssembler(deps)}
}

func (a *ContextAssembler) prepareRunMemory(ctx context.Context, req RunnerBuildRequest) (*memory.PrepareResult, error) {
	if a == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if a.deps.MemoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := a.workspaceSlug()
	result, err := a.deps.MemoryModule.Prepare(ctx, memory.PrepareRequest{
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		WorkspaceSlug: workspaceSlug,
		UserInput:     req.Input,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare memory: %w", err)
	}
	if err := a.emitRunMemoryEvents(ctx, req, workspaceSlug, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *ContextAssembler) workspaceSlug() string {
	if a.deps.Workspace == nil {
		return ""
	}
	return memory.WorkspaceSlug(a.deps.Workspace.Root())
}

func (a *ContextAssembler) emitRunMemoryEvents(ctx context.Context, req RunnerBuildRequest, workspaceSlug string, result *memory.PrepareResult) error {
	if err := emitMemoryPreparedEvent(ctx, a.deps.Store, req, memory.WorkspaceScope(workspaceSlug), result); err != nil {
		return err
	}
	return nil
}

func (a *ContextAssembler) assembleContext(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	memoryPrepared *memory.PrepareResult,
) (*cp.AssembleResult, error) {
	if a == nil || a.deps.ContextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if caps == nil {
		return nil, errors.New("run capabilities are required")
	}
	result, err := a.deps.ContextPlane.Assemble(ctx, buildAssembleRequest(req, caps, memoryPrepared))
	if err != nil {
		return nil, err
	}
	return result, nil
}

// buildAssembly dispatches to the direct_response orchestration plane,
// reusing the common baseAssemblyFields helper so agent/session/tool fields
// are not duplicated across request constructors.
func (a *ContextAssembler) buildAssembly(
	ctx context.Context,
	req RunnerBuildRequest,
	catalog *tools.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *cp.AssembleResult,
) (*RunAssembly, error) {
	if a == nil || a.deps.Config == nil {
		return nil, fmt.Errorf("runner factory is not initialized")
	}
	bf := a.baseAssemblyFields(req, catalog, chatModel, contextResult)
	return buildDirectResponse(ctx, a.deps, a.directResponseRequest(bf, req), a.toolAssembler)
}

func (a *ContextAssembler) baseAssemblyFields(req RunnerBuildRequest, catalog *tools.Catalog, chatModel einomodel.BaseChatModel, contextResult *cp.AssembleResult) baseAssemblyFields {
	return baseAssemblyFields{
		agentName:         a.deps.Config.Agent.Name,
		agentDescription:  a.deps.Config.Agent.Description,
		sessionID:         req.SessionID,
		runID:             req.RunID,
		chatModel:         chatModel,
		catalog:           catalog,
		contextResult:     AssembleResultToView(contextResult),
		allowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		excludedToolNames: append([]string(nil), req.ExcludedToolNames...),
	}
}

func (a *ContextAssembler) directResponseRequest(bf baseAssemblyFields, req RunnerBuildRequest) DirectResponseRequest {
	return DirectResponseRequest{
		AgentName:         bf.agentName,
		AgentDescription:  bf.agentDescription,
		SessionID:         bf.sessionID,
		RunID:             bf.runID,
		ChatModel:         bf.chatModel,
		AssistantStreamer: stream.NewDirectAssistantStreamer(a.deps.Store),
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

func buildAssembleRequest(req RunnerBuildRequest, caps *runCapabilities, memoryPrepared *memory.PrepareResult) cp.AssembleRequest {
	return cp.AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SkillSnapshot:  caps.skillSnapshot,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    caps.catalog,
	}
}
