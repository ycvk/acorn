package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
)

func (f *RunnerFactory) buildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.runChatModelBuilder != nil {
		return f.runChatModelBuilder(ctx, req)
	}
	// No explicit run-scoped chat model builder was injected; fall back to the
	// config-driven chat model used by NewChatModel. Usage-wrapping was removed
	// with the provider_usages table, so the raw model is returned directly.
	return f.newChatModel(ctx)
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

func (f *RunnerFactory) buildRunCapabilityAssembly(ctx context.Context, req RunnerBuildRequest) (*capabilityAssembly, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mcpManager, err := f.bootstrapRunMCP(ctx, req)
	if err != nil {
		return nil, err
	}
	capabilities, err := f.buildRunCapabilities(ctx, req.SessionID, mcpManager)
	if err != nil {
		return nil, err
	}
	return &capabilityAssembly{mcpManager: mcpManager, capabilities: capabilities}, nil
}

func (f *RunnerFactory) prepareRunMemory(ctx context.Context, req RunnerBuildRequest) (*memorymodule.PrepareResult, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.deps.MemoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := f.workspaceSlug()
	result, err := f.deps.MemoryModule.Prepare(ctx, memorymodule.PrepareRequest{
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		WorkspaceSlug: workspaceSlug,
		UserInput:     req.Input,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare memory: %w", err)
	}
	if err := f.emitRunMemoryEvents(ctx, req, workspaceSlug, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (f *RunnerFactory) workspaceSlug() string {
	if f.deps.Workspace == nil {
		return ""
	}
	return memorymodule.WorkspaceSlug(f.deps.Workspace.Root())
}

func (f *RunnerFactory) emitRunMemoryEvents(ctx context.Context, req RunnerBuildRequest, workspaceSlug string, result *memorymodule.PrepareResult) error {
	if err := emitMemoryPreparedEvent(ctx, f.deps.Store, req, memorymodule.WorkspaceScope(workspaceSlug), result); err != nil {
		return err
	}
	return nil
}

func (f *RunnerFactory) assembleContext(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	selection *runSelection,
	memoryPrepared *memorymodule.PrepareResult,
) (*contextplane.AssembleResult, error) {
	if f == nil || f.deps.ContextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if caps == nil {
		return nil, errors.New("run capabilities are required")
	}
	result, err := f.deps.ContextPlane.Assemble(ctx, buildAssembleRequest(req, caps, selection, memoryPrepared))
	if err != nil {
		return nil, err
	}
	return result, nil
}

func buildAssembleRequest(req RunnerBuildRequest, caps *runCapabilities, selection *runSelection, memoryPrepared *memorymodule.PrepareResult) contextplane.AssembleRequest {
	var selectedSkill *SelectedSkill
	if selection != nil {
		selectedSkill = selection.selectedSkill
	}
	return contextplane.AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SelectedSkill:  selectedSkill,
		SkillSnapshot:  caps.skillSnapshot,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    caps.catalog,
	}
}
