package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/contextplane/compaction"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/plan"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
)

type orchestrationPlane interface {
	BuildDirectResponse(ctx context.Context, req orchestration.DirectResponseRequest) (*orchestration.RunAssembly, error)
	BuildSingleAgent(ctx context.Context, req orchestration.SingleAgentRequest) (*orchestration.RunAssembly, error)
	BuildPlanExecute(ctx context.Context, req orchestration.PlanExecuteRequest) (*orchestration.RunAssembly, error)
}

type defaultOrchestrationPlaneDeps struct {
	cfg          *config.Config
	store        RunnerFactoryStore
	contextPlane contextplane.ContextPlane
	handlers     []adk.ChatModelAgentMiddleware
}

func newDefaultOrchestrationPlane(deps defaultOrchestrationPlaneDeps) *orchestration.DefaultPlane {
	return orchestration.NewDefaultPlane(orchestration.DefaultPlaneOptions{
		SystemPrompt:            deps.cfg.Agent.SystemPrompt,
		MaxIterations:           deps.cfg.Agent.MaxIterations,
		CheckpointStore:         deps.store,
		PlanStore:               plan.NewPlanStore(deps.store),
		ToolBuilder:             deps.buildAuditedTools,
		ToolNodeFactory:         deps.buildToolNode,
		GraphBuilder:            BuildRuntimeAgentGraph,
		PlanExecuteGraphBuilder: BuildRuntimePlanExecuteGraph,
		HandlersBuilder:         deps.buildHandlers,
		InstructionBuilder:      buildStableInstruction,
		ToolLifecycleBinder:     deps.bindToolLifecycle,
		SessionContextBinder:    bindSessionID,
	})
}

func (d defaultOrchestrationPlaneDeps) buildAuditedTools(
	ctx context.Context,
	specs []tooling.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	runID string,
) ([]einotool.BaseTool, error) {
	return tool.BuildAuditedTools(ctx, d.store, specs, excludedToolNames, allowedToolNames, runID)
}

func (d defaultOrchestrationPlaneDeps) buildToolNode(
	ctx context.Context,
	tools []einotool.BaseTool,
	resolver tooling.ExecutionPolicyResolver,
) (orchestration.ToolInvoker, error) {
	return tool.NewSafeParallelToolsNode(ctx, tools, resolver)
}

func (d defaultOrchestrationPlaneDeps) buildHandlers(
	ctx context.Context,
	chatModel einomodel.BaseChatModel,
	compressionState any,
) ([]adk.ChatModelAgentMiddleware, error) {
	return buildRunnerAgentHandlers(ctx, d.cfg, d.contextPlane, d.handlers, chatModel, compressionState)
}

func buildRunnerAgentHandlers(
	ctx context.Context,
	cfg *config.Config,
	contextPlane contextplane.ContextPlane,
	extraHandlers []adk.ChatModelAgentMiddleware,
	chatModel einomodel.BaseChatModel,
	compressionState any,
) ([]adk.ChatModelAgentMiddleware, error) {
	if cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if contextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	contextPolicy, err := cfg.ContextPolicy()
	if err != nil {
		return nil, fmt.Errorf("context policy: %w", err)
	}
	compressionHandlers, err := compaction.NewCompressionMiddlewareBuilder().Build(ctx, contextPolicy, chatModel, contextplane.CompressionBuildOptions{
		RuntimeStorageDir: cfg.Runtime.StorageDir,
		State:             compressionState,
	})
	if err != nil {
		return nil, err
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, len(compressionHandlers)+len(extraHandlers)+2)
	handlers = append(handlers, compressionHandlers...)
	handlers = append(handlers, extraHandlers...)
	return handlers, nil
}

func (d defaultOrchestrationPlaneDeps) bindToolLifecycle(
	ctx context.Context,
	state orchestration.ToolLifecycleStateView,
	catalog *tooling.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	if adapter, ok := state.(toolLifecycleStateAdapter); ok && adapter.state != nil {
		return contextplane.WithToolLifecycleContext(ctx, d.contextPlane.ToolResultLedger(), adapter.state, catalog, infos)
	}
	return ctx
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return runtimeapi.WithSessionID(ctx, sessionID)
}
