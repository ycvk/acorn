package orchestration

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/tooling"
)

type StreamingExecutor interface {
	Submit(call schema.ToolCall)
	GetRemainingResults(ctx context.Context) ([]*schema.Message, error)
	Discard()
}

type ToolInvoker interface {
	NewStreamingExecutor(ctx context.Context) StreamingExecutor
}

type PlanStore interface {
	OrchestrationPlanStore()
}

type GraphBuildRequest struct {
	AgentName              string
	AgentDescription       string
	ChatModel              einomodel.BaseChatModel
	Tools                  []einotool.BaseTool
	SafeToolNode           ToolInvoker
	AssistantStreamer      AssistantStreamer
	MaxIterations          int
	Handlers               []adk.ChatModelAgentMiddleware
	CheckpointStore        adk.CheckPointStore
	PlanStore              PlanStore
	PlanPrompt             string
	PlanningPromptProvider PlanningPromptProvider
	EagerToolNames         []string
	ToolSpecs              []tooling.ToolSpec
}

type PlanExecuteGraphBuildRequest struct {
	AgentName              string
	AgentDescription       string
	ChatModel              einomodel.BaseChatModel
	Tools                  []einotool.BaseTool
	MaxIterations          int
	Handlers               []adk.ChatModelAgentMiddleware
	CheckpointStore        adk.CheckPointStore
	PlanStore              PlanStore
	PlanPrompt             string
	PlanningPromptProvider PlanningPromptProvider
	EagerToolNames         []string
	ToolSpecs              []tooling.ToolSpec
	ChildExecutor          ChildAgentExecutor
}

type GraphBuilder func(ctx context.Context, req GraphBuildRequest) (adk.Agent, error)

type PlanExecuteGraphBuilder func(ctx context.Context, req PlanExecuteGraphBuildRequest) (adk.Agent, error)

type ToolNodeFactory func(ctx context.Context, tools []einotool.BaseTool, resolver tooling.ExecutionPolicyResolver) (ToolInvoker, error)

type ToolBuilder func(
	ctx context.Context,
	specs []tooling.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	runID string,
) ([]einotool.BaseTool, error)

type InstructionBuilder func(base string, suffix string) string

type InstructionCacheInvalidator func()

type ToolSchemaChangeDetector func(ctx context.Context, tools []einotool.BaseTool) bool

type HandlersBuilder func(ctx context.Context, chatModel einomodel.BaseChatModel, compressionState *contextplane.CompressionState) ([]adk.ChatModelAgentMiddleware, error)

type DefaultPlaneOptions struct {
	SystemPrompt                string
	MaxIterations               int
	CheckpointStore             adk.CheckPointStore
	PlanStore                   PlanStore
	ToolBuilder                 ToolBuilder
	ToolNodeFactory             ToolNodeFactory
	GraphBuilder                GraphBuilder
	PlanExecuteGraphBuilder     PlanExecuteGraphBuilder
	HandlersBuilder             HandlersBuilder
	InstructionBuilder          InstructionBuilder
	InstructionCacheInvalidator InstructionCacheInvalidator
	ToolSchemaChangeDetector    ToolSchemaChangeDetector
	ToolLifecycleBinder         ToolLifecycleBinder
	StoreContextBinder          func(ctx context.Context) context.Context
	SessionContextBinder        func(ctx context.Context, sessionID string) context.Context
}

type DefaultPlane struct {
	systemPrompt                string
	maxIterations               int
	checkpointStore             adk.CheckPointStore
	planStore                   PlanStore
	toolBuilder                 ToolBuilder
	toolNodeFactory             ToolNodeFactory
	graphBuilder                GraphBuilder
	planExecuteGraphBuilder     PlanExecuteGraphBuilder
	handlersBuilder             HandlersBuilder
	instructionBuilder          InstructionBuilder
	instructionCacheInvalidator InstructionCacheInvalidator
	toolSchemaChangeDetector    ToolSchemaChangeDetector
	toolLifecycleBinder         ToolLifecycleBinder
	storeContextBinder          func(ctx context.Context) context.Context
	sessionContextBinder        func(ctx context.Context, sessionID string) context.Context
}

func NewDefaultPlane(opts DefaultPlaneOptions) *DefaultPlane {
	return &DefaultPlane{
		systemPrompt:                opts.SystemPrompt,
		maxIterations:               opts.MaxIterations,
		checkpointStore:             opts.CheckpointStore,
		planStore:                   opts.PlanStore,
		toolBuilder:                 opts.ToolBuilder,
		toolNodeFactory:             opts.ToolNodeFactory,
		graphBuilder:                opts.GraphBuilder,
		planExecuteGraphBuilder:     opts.PlanExecuteGraphBuilder,
		handlersBuilder:             opts.HandlersBuilder,
		instructionBuilder:          opts.InstructionBuilder,
		instructionCacheInvalidator: opts.InstructionCacheInvalidator,
		toolSchemaChangeDetector:    opts.ToolSchemaChangeDetector,
		toolLifecycleBinder:         opts.ToolLifecycleBinder,
		storeContextBinder:          opts.StoreContextBinder,
		sessionContextBinder:        opts.SessionContextBinder,
	}
}

func (p *DefaultPlane) BuildSingleAgent(ctx context.Context, req SingleAgentRequest) (*SingleAgentAssembly, error) {
	if p == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	if req.Catalog == nil {
		return nil, fmt.Errorf("tool catalog is required")
	}
	if req.AssistantStreamer == nil {
		return nil, fmt.Errorf("assistant streamer is required")
	}
	if req.ContextResult == nil || req.ContextResult.LifecycleState == nil {
		return nil, fmt.Errorf("context plane lifecycle state is required")
	}
	if p.toolBuilder == nil || p.toolNodeFactory == nil || p.graphBuilder == nil || p.handlersBuilder == nil || p.instructionBuilder == nil {
		return nil, fmt.Errorf("orchestration plane is missing required dependencies")
	}
	if p.checkpointStore == nil {
		return nil, fmt.Errorf("orchestration plane requires checkpoint store")
	}
	if p.planStore == nil {
		return nil, fmt.Errorf("orchestration plane requires plan store")
	}

	allTools, err := p.toolBuilder(
		ctx,
		req.Catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
		req.ExcludedToolNames,
		req.AllowedToolNames,
		req.RunID,
	)
	if err != nil {
		return nil, err
	}

	toolInfos := make([]*schema.ToolInfo, 0, len(allTools))
	for _, tool := range allTools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info for BindTools: %w", err)
		}
		toolInfos = append(toolInfos, info)
	}

	if p.toolSchemaChangeDetector != nil && p.toolSchemaChangeDetector(ctx, allTools) && p.instructionCacheInvalidator != nil {
		p.instructionCacheInvalidator()
	}

	instruction := p.instructionBuilder(p.systemPrompt, req.InstructionSuffix)
	compressionState := contextplane.NewCompressionState()
	handlers, err := p.handlersBuilder(ctx, req.ChatModel, compressionState)
	if err != nil {
		return nil, err
	}

	safeToolNode, err := p.toolNodeFactory(ctx, allTools, req.Catalog)
	if err != nil {
		return nil, fmt.Errorf("build safe parallel tools node: %w", err)
	}

	runCtx := ctx
	if p.storeContextBinder != nil {
		runCtx = p.storeContextBinder(runCtx)
	}
	if p.sessionContextBinder != nil {
		runCtx = p.sessionContextBinder(runCtx, req.SessionID)
	}
	if p.toolLifecycleBinder != nil {
		runCtx = p.toolLifecycleBinder(runCtx, req.ContextResult.LifecycleState, req.Catalog, toolInfos)
	}

	agent, err := p.graphBuilder(runCtx, GraphBuildRequest{
		AgentName:              req.AgentName,
		AgentDescription:       req.AgentDescription,
		ChatModel:              req.ChatModel,
		Tools:                  append([]einotool.BaseTool(nil), allTools...),
		SafeToolNode:           safeToolNode,
		AssistantStreamer:      req.AssistantStreamer,
		MaxIterations:          p.maxIterations,
		Handlers:               append([]adk.ChatModelAgentMiddleware(nil), handlers...),
		CheckpointStore:        p.checkpointStore,
		PlanStore:              p.planStore,
		PlanPrompt:             instruction,
		PlanningPromptProvider: req.PlanningPromptProvider,
		EagerToolNames:         append([]string(nil), req.ContextResult.EagerToolNames...),
		ToolSpecs:              req.Catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
	})
	if err != nil {
		return nil, fmt.Errorf("build agent graph: %w", err)
	}

	return &SingleAgentAssembly{
		Runner: adk.NewRunner(runCtx, adk.RunnerConfig{
			Agent:           agent,
			EnableStreaming: false,
			CheckPointStore: p.checkpointStore,
		}),
		Instruction:      instruction,
		CompressionState: compressionState,
	}, nil
}

func (p *DefaultPlane) BuildPlanExecute(ctx context.Context, req PlanExecuteRequest) (*RunAssembly, error) {
	if p == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	if req.Catalog == nil {
		return nil, fmt.Errorf("tool catalog is required")
	}
	if req.ContextResult == nil || req.ContextResult.LifecycleState == nil {
		return nil, fmt.Errorf("context plane lifecycle state is required")
	}
	if p.toolBuilder == nil || p.handlersBuilder == nil || p.instructionBuilder == nil || p.planExecuteGraphBuilder == nil {
		return nil, fmt.Errorf("orchestration plane is missing required dependencies")
	}
	if p.checkpointStore == nil {
		return nil, fmt.Errorf("orchestration plane requires checkpoint store")
	}
	if p.planStore == nil {
		return nil, fmt.Errorf("orchestration plane requires plan store")
	}
	if req.ChildExecutor == nil {
		return nil, fmt.Errorf("plan-execute child executor is required")
	}

	allTools, err := p.toolBuilder(
		ctx,
		req.Catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
		req.ExcludedToolNames,
		req.AllowedToolNames,
		req.RunID,
	)
	if err != nil {
		return nil, err
	}

	toolInfos := make([]*schema.ToolInfo, 0, len(allTools))
	for _, tool := range allTools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info for BindTools: %w", err)
		}
		toolInfos = append(toolInfos, info)
	}

	if p.toolSchemaChangeDetector != nil && p.toolSchemaChangeDetector(ctx, allTools) && p.instructionCacheInvalidator != nil {
		p.instructionCacheInvalidator()
	}

	instruction := p.instructionBuilder(p.systemPrompt, req.InstructionSuffix)
	compressionState := contextplane.NewCompressionState()
	handlers, err := p.handlersBuilder(ctx, req.ChatModel, compressionState)
	if err != nil {
		return nil, err
	}

	runCtx := ctx
	if p.storeContextBinder != nil {
		runCtx = p.storeContextBinder(runCtx)
	}
	if p.sessionContextBinder != nil {
		runCtx = p.sessionContextBinder(runCtx, req.SessionID)
	}
	if p.toolLifecycleBinder != nil {
		runCtx = p.toolLifecycleBinder(runCtx, req.ContextResult.LifecycleState, req.Catalog, toolInfos)
	}

	agent, err := p.planExecuteGraphBuilder(runCtx, PlanExecuteGraphBuildRequest{
		AgentName:              req.AgentName,
		AgentDescription:       req.AgentDescription,
		ChatModel:              req.ChatModel,
		Tools:                  append([]einotool.BaseTool(nil), allTools...),
		MaxIterations:          p.maxIterations,
		Handlers:               append([]adk.ChatModelAgentMiddleware(nil), handlers...),
		CheckpointStore:        p.checkpointStore,
		PlanStore:              p.planStore,
		PlanPrompt:             instruction,
		PlanningPromptProvider: req.PlanningPromptProvider,
		EagerToolNames:         append([]string(nil), req.ContextResult.EagerToolNames...),
		ToolSpecs:              req.Catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
		ChildExecutor:          req.ChildExecutor,
	})
	if err != nil {
		return nil, fmt.Errorf("build plan-execute graph: %w", err)
	}

	return &RunAssembly{
		Runner: adk.NewRunner(runCtx, adk.RunnerConfig{
			Agent:           agent,
			EnableStreaming: false,
			CheckPointStore: p.checkpointStore,
		}),
		Instruction:      instruction,
		CompressionState: compressionState,
	}, nil
}
