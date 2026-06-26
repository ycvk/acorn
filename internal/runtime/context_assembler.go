package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/memory"
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
