package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

type modelProviderAssembler struct {
	factory  *RunnerFactory
	buildRun func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
}

func (a *modelProviderAssembler) BuildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
	if a == nil || a.factory == nil || a.factory.cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if a.buildRun != nil {
		return a.buildRun(ctx, req)
	}
	model, provider, err := buildRuntimeChatModelWithProvider(ctx, a.factory.cfg, nil)
	if err != nil {
		return nil, err
	}
	metadata := providerusage.RunMetadata{
		RunID:        req.RunID,
		SessionID:    req.SessionID,
		ProviderName: provider.Name,
		ModelName:    provider.Model,
	}
	if req.RunID != "" && a.factory.store != nil {
		existing, err := a.factory.store.ListProviderUsagesByRun(ctx, req.RunID)
		if err == nil {
			metadata.InitialSequence = uint64(len(existing))
		}
	}
	return providerusage.WrapModel(model, a.factory.store, metadata)
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
	if a.factory.memoryModuleErr != nil {
		return nil, fmt.Errorf("memory module initialization: %w", a.factory.memoryModuleErr)
	}
	if a.factory.memoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := ""
	if a.factory.workspace != nil {
		workspaceSlug = memorymodule.WorkspaceSlug(a.factory.workspace.Root())
	}
	result, err := a.factory.memoryModule.Prepare(ctx, memorymodule.PrepareRequest{
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		WorkspaceSlug: workspaceSlug,
		UserInput:     req.Input,
		Mode:          string(req.OrchestrationMode),
	})
	if err != nil {
		return nil, fmt.Errorf("prepare memory: %w", err)
	}
	if err := emitMemoryPreparedEvent(ctx, a.factory.store, req, memorymodule.WorkspaceScope(workspaceSlug), result); err != nil {
		return nil, err
	}
	if result != nil {
		if err := emitProcedureActivationEvents(ctx, a.factory.store, req.Sink, req.RunID, result.ProcedureActivations); err != nil {
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
	if a == nil || a.factory == nil || a.factory.contextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if a.factory.contextPlaneErr != nil {
		return nil, a.factory.contextPlaneErr
	}
	if caps == nil {
		return nil, errors.New("run capabilities are required")
	}
	if selection == nil {
		selection = &runSelection{}
	}
	result, err := a.factory.contextPlane.Assemble(ctx, contextplane.AssembleRequest{
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
		a.factory.store,
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
	if a == nil || a.factory == nil || a.factory.contextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if a.factory.contextPlaneErr != nil {
		return nil, a.factory.contextPlaneErr
	}
	result, err := a.factory.contextPlane.Assemble(ctx, contextplane.AssembleRequest{
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
		a.factory.store,
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
