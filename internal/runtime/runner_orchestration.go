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
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/tooling"
)

type defaultOrchestrationPlaneDeps struct {
	cfg          *config.Config
	store        RunnerFactoryStore
	contextPlane contextplane.ContextPlane
	handlers     []adk.ChatModelAgentMiddleware
}

func newDefaultOrchestrationPlane(deps defaultOrchestrationPlaneDeps) *orchestration.DefaultPlane {
	toolSchemaCache := NewToolSchemaCache()
	return orchestration.NewDefaultPlane(orchestration.DefaultPlaneOptions{
		SystemPrompt:             deps.cfg.Agent.SystemPrompt,
		MaxIterations:            deps.cfg.Agent.MaxIterations,
		CheckpointStore:          deps.store,
		PlanStore:                NewPlanStore(deps.store),
		ToolBuilder:              deps.buildAuditedTools,
		ToolNodeFactory:          deps.buildToolNode,
		GraphBuilder:             BuildRuntimeAgentGraph,
		PlanExecuteGraphBuilder:  BuildRuntimePlanExecuteGraph,
		HandlersBuilder:          deps.buildHandlers,
		InstructionBuilder:       buildStableInstruction,
		ToolSchemaChangeDetector: toolSchemaCache.AnyChanged,
		ToolLifecycleBinder:      deps.bindToolLifecycle,
		StoreContextBinder:       deps.bindStore,
		SessionContextBinder:     bindSessionID,
	})
}

func (d defaultOrchestrationPlaneDeps) buildAuditedTools(
	ctx context.Context,
	specs []tooling.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	runID string,
) ([]einotool.BaseTool, error) {
	return buildAuditedTools(
		ctx,
		d.store,
		specs,
		excludedToolNames,
		allowedToolNames,
		runID,
	)
}

func (d defaultOrchestrationPlaneDeps) buildToolNode(
	ctx context.Context,
	tools []einotool.BaseTool,
	resolver tooling.ExecutionPolicyResolver,
) (orchestration.ToolInvoker, error) {
	return NewSafeParallelToolsNode(ctx, tools, resolver)
}

func BuildRuntimeAgentGraph(ctx context.Context, req orchestration.GraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, typedPromptProvider, err := runtimeGraphDependencies(req.PlanStore, req.PlanningPromptProvider)
	if err != nil {
		return nil, err
	}
	runnable, err := BuildAgentGraph(
		ctx,
		req.AgentName,
		req.ChatModel,
		req.SafeToolNode,
		req.AssistantStreamer,
		req.MaxIterations,
		req.CheckpointStore,
		req.Handlers,
		typedPlanStore,
		req.PlanPrompt,
		typedPromptProvider,
		req.EagerToolNames,
		req.ToolSpecs,
	)
	if err != nil {
		return nil, err
	}
	return graph.NewGraphAgent(
		req.AgentName,
		req.AgentDescription,
		runnable,
		req.ChatModel,
		req.Tools,
		req.Handlers,
		req.MaxIterations,
		req.CheckpointStore,
		graph.GraphAgentContextBinder(ctx),
	), nil
}

func BuildRuntimePlanExecuteGraph(ctx context.Context, req orchestration.PlanExecuteGraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, typedPromptProvider, err := runtimeGraphDependencies(req.PlanStore, req.PlanningPromptProvider)
	if err != nil {
		return nil, err
	}
	runnable, err := BuildPlanExecuteGraph(
		ctx,
		req.AgentName,
		req.ChatModel,
		req.MaxIterations,
		req.CheckpointStore,
		req.Handlers,
		typedPlanStore,
		req.PlanPrompt,
		typedPromptProvider,
		req.EagerToolNames,
		req.ToolSpecs,
		req.ChildExecutor,
	)
	if err != nil {
		return nil, err
	}
	return graph.NewGraphAgent(
		req.AgentName,
		req.AgentDescription,
		runnable,
		req.ChatModel,
		req.Tools,
		req.Handlers,
		req.MaxIterations,
		req.CheckpointStore,
		graph.GraphAgentContextBinder(ctx),
	), nil
}

func runtimeGraphDependencies(
	planStore orchestration.PlanStore,
	promptProvider orchestration.PlanningPromptProvider,
) (runtimeapi.PlanStore, PlanningPromptProvider, error) {
	typedPlanStore, ok := planStore.(runtimeapi.PlanStore)
	if !ok {
		return nil, nil, fmt.Errorf("orchestration plane requires runtime plan store")
	}
	typedPromptProvider, ok := promptProvider.(PlanningPromptProvider)
	if promptProvider != nil && !ok {
		return nil, nil, fmt.Errorf("orchestration plane requires runtime planning prompt provider")
	}
	return typedPlanStore, typedPromptProvider, nil
}

func (d defaultOrchestrationPlaneDeps) buildHandlers(
	ctx context.Context,
	chatModel einomodel.BaseChatModel,
	compressionState any,
) ([]adk.ChatModelAgentMiddleware, error) {
	return buildRunnerAgentHandlers(
		ctx,
		d.cfg,
		d.contextPlane,
		d.handlers,
		d.store,
		chatModel,
		compressionState,
	)
}

func buildRunnerAgentHandlers(
	ctx context.Context,
	cfg *config.Config,
	contextPlane contextplane.ContextPlane,
	extraHandlers []adk.ChatModelAgentMiddleware,
	store runtimeapi.EventAppender,
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
	compressionHandlers, err := contextPlane.BuildHandlers(ctx, contextPolicy, chatModel, contextplane.CompressionBuildOptions{
		RuntimeStorageDir: cfg.Runtime.StorageDir,
		State:             compressionState,
		EmitCompressed: func(ctx context.Context, outcome contextplane.CompressionOutcome) error {
			return EmitContextCompressedEvent(ctx, store, outcome)
		},
		EmitPressure: func(ctx context.Context, pressure contextplane.BudgetPressure) error {
			return EmitContextPressureEvent(ctx, store, pressure)
		},
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
		return contextplane.WithToolLifecycleContext(ctx, d.contextPlane, adapter.state, catalog, infos)
	}
	return ctx
}

func (d defaultOrchestrationPlaneDeps) bindStore(ctx context.Context) context.Context {
	return runtimeapi.WithStore(ctx, d.store)
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return runtimeapi.WithSessionID(ctx, sessionID)
}

type toolLifecycleStateAdapter struct {
	state *contextplane.ToolLifecycleState
}

func (a toolLifecycleStateAdapter) IsLoaded(toolName string) bool {
	if a.state == nil {
		return false
	}
	_, ok := a.state.LoadedTools[toolName]
	return ok
}

func AssembleResultToView(result *contextplane.AssembleResult) orchestration.AssembleResultView {
	if result == nil {
		return orchestration.AssembleResultView{}
	}
	return orchestration.AssembleResultView{
		Messages:          result.Messages,
		LifecycleState:    toolLifecycleStateAdapter{state: result.LifecycleState},
		EagerToolNames:    result.EagerToolNames,
		DeferredToolNames: result.DeferredToolNames,
	}
}

const systemHotReloadRunID = "_system_hot_reload"
