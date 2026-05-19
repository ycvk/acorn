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
	"github.com/ycvk/acorn/internal/tooling"
)

type defaultOrchestrationPlaneDeps struct {
	cfg          *config.Config
	store        runnerFactoryStore
	contextPlane contextplane.ContextPlane
	handlers     []adk.ChatModelAgentMiddleware
}

func newDefaultOrchestrationPlane(deps defaultOrchestrationPlaneDeps) *orchestration.DefaultPlane {
	toolSchemaCache := NewToolSchemaCache()
	return orchestration.NewDefaultPlane(orchestration.DefaultPlaneOptions{
		SystemPrompt:             deps.cfg.Agent.SystemPrompt,
		MaxIterations:            deps.cfg.Agent.MaxIterations,
		CheckpointStore:          deps.store,
		PlanStore:                newPlanStore(deps.store),
		ToolBuilder:              deps.buildAuditedTools,
		ToolNodeFactory:          deps.buildToolNode,
		GraphBuilder:             buildRuntimeAgentGraph,
		PlanExecuteGraphBuilder:  buildRuntimePlanExecuteGraph,
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

func buildRuntimeAgentGraph(ctx context.Context, req orchestration.GraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, typedPromptProvider, err := runtimeGraphDependencies(req.PlanStore, req.PlanningPromptProvider)
	if err != nil {
		return nil, err
	}
	runnable, err := buildAgentGraph(
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
	return newGraphAgent(
		req.AgentName,
		req.AgentDescription,
		runnable,
		req.ChatModel,
		req.Tools,
		req.Handlers,
		req.MaxIterations,
		req.CheckpointStore,
		graphAgentContextBinder(ctx),
	), nil
}

func buildRuntimePlanExecuteGraph(ctx context.Context, req orchestration.PlanExecuteGraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, typedPromptProvider, err := runtimeGraphDependencies(req.PlanStore, req.PlanningPromptProvider)
	if err != nil {
		return nil, err
	}
	runnable, err := buildPlanExecuteGraph(
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
	return newGraphAgent(
		req.AgentName,
		req.AgentDescription,
		runnable,
		req.ChatModel,
		req.Tools,
		req.Handlers,
		req.MaxIterations,
		req.CheckpointStore,
		graphAgentContextBinder(ctx),
	), nil
}

func runtimeGraphDependencies(
	planStore orchestration.PlanStore,
	promptProvider orchestration.PlanningPromptProvider,
) (PlanStore, PlanningPromptProvider, error) {
	typedPlanStore, ok := planStore.(PlanStore)
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
	compressionState *contextplane.CompressionState,
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
	store eventAppender,
	chatModel einomodel.BaseChatModel,
	compressionState *contextplane.CompressionState,
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
			return emitContextCompressedEvent(ctx, store, outcome)
		},
		EmitPressure: func(ctx context.Context, pressure contextplane.BudgetPressure) error {
			return emitContextPressureEvent(ctx, store, pressure)
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
	state *contextplane.ToolLifecycleState,
	catalog *tooling.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	return contextplane.WithToolLifecycleContext(ctx, d.contextPlane, state, catalog, infos)
}

func (d defaultOrchestrationPlaneDeps) bindStore(ctx context.Context) context.Context {
	return withStore(ctx, d.store)
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return withSessionID(ctx, sessionID)
}
