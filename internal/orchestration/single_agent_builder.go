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
	AgentName         string
	AgentDescription  string
	ChatModel         einomodel.BaseChatModel
	Tools             []einotool.BaseTool
	SafeToolNode      ToolInvoker
	AssistantStreamer AssistantStreamer
	MaxIterations     int
	Handlers          []adk.ChatModelAgentMiddleware
	CheckpointStore   adk.CheckPointStore
	PlanStore         PlanStore
	PlanPrompt        string
	EagerToolNames    []string
	ToolSpecs         []tooling.ToolSpec
}

type PlanExecuteGraphBuildRequest struct {
	AgentName        string
	AgentDescription string
	ChatModel        einomodel.BaseChatModel
	Tools            []einotool.BaseTool
	MaxIterations    int
	Handlers         []adk.ChatModelAgentMiddleware
	CheckpointStore  adk.CheckPointStore
	PlanStore        PlanStore
	PlanPrompt       string
	EagerToolNames   []string
	ToolSpecs        []tooling.ToolSpec
	ChildExecutor    ChildAgentExecutor
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

type HandlersBuilder func(ctx context.Context, chatModel einomodel.BaseChatModel, compressionState any) ([]adk.ChatModelAgentMiddleware, error)

type DefaultPlaneOptions struct {
	SystemPrompt            string
	MaxIterations           int
	CheckpointStore         adk.CheckPointStore
	PlanStore               PlanStore
	ToolBuilder             ToolBuilder
	ToolNodeFactory         ToolNodeFactory
	GraphBuilder            GraphBuilder
	PlanExecuteGraphBuilder PlanExecuteGraphBuilder
	HandlersBuilder         HandlersBuilder
	InstructionBuilder      InstructionBuilder
	ToolLifecycleBinder     ToolLifecycleBinder
	SessionContextBinder    func(ctx context.Context, sessionID string) context.Context
}

type DefaultPlane struct {
	systemPrompt            string
	maxIterations           int
	checkpointStore         adk.CheckPointStore
	planStore               PlanStore
	toolBuilder             ToolBuilder
	toolNodeFactory         ToolNodeFactory
	graphBuilder            GraphBuilder
	planExecuteGraphBuilder PlanExecuteGraphBuilder
	handlersBuilder         HandlersBuilder
	instructionBuilder      InstructionBuilder
	toolLifecycleBinder     ToolLifecycleBinder
	sessionContextBinder    func(ctx context.Context, sessionID string) context.Context
}

func NewDefaultPlane(opts DefaultPlaneOptions) *DefaultPlane {
	return &DefaultPlane{
		systemPrompt:            opts.SystemPrompt,
		maxIterations:           opts.MaxIterations,
		checkpointStore:         opts.CheckpointStore,
		planStore:               opts.PlanStore,
		toolBuilder:             opts.ToolBuilder,
		toolNodeFactory:         opts.ToolNodeFactory,
		graphBuilder:            opts.GraphBuilder,
		planExecuteGraphBuilder: opts.PlanExecuteGraphBuilder,
		handlersBuilder:         opts.HandlersBuilder,
		instructionBuilder:      opts.InstructionBuilder,
		toolLifecycleBinder:     opts.ToolLifecycleBinder,
		sessionContextBinder:    opts.SessionContextBinder,
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
	if req.ContextResult.LifecycleState == nil {
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

	assembled, err := p.assembleTooling(ctx, toolAssemblyParams{
		catalog:           req.Catalog,
		contextResult:     req.ContextResult,
		allowedToolNames:  req.AllowedToolNames,
		excludedToolNames: req.ExcludedToolNames,
		runID:             req.RunID,
		chatModel:         req.ChatModel,
		instructionSuffix: req.InstructionSuffix,
		sessionID:         req.SessionID,
	})
	if err != nil {
		return nil, err
	}

	safeToolNode, err := p.toolNodeFactory(ctx, assembled.allTools, req.Catalog)
	if err != nil {
		return nil, fmt.Errorf("build safe parallel tools node: %w", err)
	}

	agent, err := p.graphBuilder(assembled.runCtx, GraphBuildRequest{
		AgentName:         req.AgentName,
		AgentDescription:  req.AgentDescription,
		ChatModel:         req.ChatModel,
		Tools:             append([]einotool.BaseTool(nil), assembled.allTools...),
		SafeToolNode:      safeToolNode,
		AssistantStreamer: req.AssistantStreamer,
		MaxIterations:     p.maxIterations,
		Handlers:          append([]adk.ChatModelAgentMiddleware(nil), assembled.handlers...),
		CheckpointStore:   p.checkpointStore,
		PlanStore:         p.planStore,
		PlanPrompt:        assembled.instruction,
		EagerToolNames:    append([]string(nil), req.ContextResult.EagerToolNames...),
		ToolSpecs:         req.Catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
	})
	if err != nil {
		return nil, fmt.Errorf("build agent graph: %w", err)
	}

	return &SingleAgentAssembly{
		Runner: adk.NewRunner(assembled.runCtx, adk.RunnerConfig{
			Agent:           agent,
			EnableStreaming: false,
			CheckPointStore: p.checkpointStore,
		}),
		Instruction:      assembled.instruction,
		CompressionState: assembled.compressionState,
	}, nil
}

func (p *DefaultPlane) BuildPlanExecute(ctx context.Context, req PlanExecuteRequest) (*RunAssembly, error) {
	if p == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	if req.Catalog == nil {
		return nil, fmt.Errorf("tool catalog is required")
	}
	if req.ContextResult.LifecycleState == nil {
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

	assembled, err := p.assembleTooling(ctx, toolAssemblyParams{
		catalog:           req.Catalog,
		contextResult:     req.ContextResult,
		allowedToolNames:  req.AllowedToolNames,
		excludedToolNames: req.ExcludedToolNames,
		runID:             req.RunID,
		chatModel:         req.ChatModel,
		instructionSuffix: req.InstructionSuffix,
		sessionID:         req.SessionID,
	})
	if err != nil {
		return nil, err
	}

	agent, err := p.planExecuteGraphBuilder(assembled.runCtx, PlanExecuteGraphBuildRequest{
		AgentName:        req.AgentName,
		AgentDescription: req.AgentDescription,
		ChatModel:        req.ChatModel,
		Tools:            append([]einotool.BaseTool(nil), assembled.allTools...),
		MaxIterations:    p.maxIterations,
		Handlers:         append([]adk.ChatModelAgentMiddleware(nil), assembled.handlers...),
		CheckpointStore:  p.checkpointStore,
		PlanStore:        p.planStore,
		PlanPrompt:       assembled.instruction,
		EagerToolNames:   append([]string(nil), req.ContextResult.EagerToolNames...),
		ToolSpecs:        req.Catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
		ChildExecutor:    req.ChildExecutor,
	})
	if err != nil {
		return nil, fmt.Errorf("build plan-execute graph: %w", err)
	}

	return &RunAssembly{
		Runner: adk.NewRunner(assembled.runCtx, adk.RunnerConfig{
			Agent:           agent,
			EnableStreaming: false,
			CheckPointStore: p.checkpointStore,
		}),
		Instruction:      assembled.instruction,
		CompressionState: assembled.compressionState,
	}, nil
}

// toolAssemblyParams holds the fields BuildSingleAgent and BuildPlanExecute share
// when assembling tools, instruction, handlers, and the bound run context.
type toolAssemblyParams struct {
	catalog           *tooling.Catalog
	contextResult     AssembleResultView
	allowedToolNames  []string
	excludedToolNames []string
	runID             string
	chatModel         einomodel.BaseChatModel
	instructionSuffix string
	sessionID         string
}

type assembledTooling struct {
	allTools         []einotool.BaseTool
	toolInfos        []*schema.ToolInfo
	instruction      string
	compressionState *contextplane.CompressionState
	handlers         []adk.ChatModelAgentMiddleware
	runCtx           context.Context
}

// assembleTooling builds the tool set, instruction, handlers, and the run context
// bound with session + tool lifecycle — the prefix shared verbatim by the
// single-agent and plan-execute graph builders. The graph wiring itself stays in
// each builder (single-agent adds a SafeToolNode; plan-execute adds a child
// executor), so only the duplicated assembly is factored out here.
func (p *DefaultPlane) assembleTooling(ctx context.Context, params toolAssemblyParams) (*assembledTooling, error) {
	allTools, err := p.toolBuilder(
		ctx,
		params.catalog.EnabledSpecsForProfile(tooling.ToolProfileRun),
		params.excludedToolNames,
		params.allowedToolNames,
		params.runID,
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

	instruction := p.instructionBuilder(p.systemPrompt, params.instructionSuffix)
	compressionState := contextplane.NewCompressionState()
	var handlers []adk.ChatModelAgentMiddleware
	if p.handlersBuilder != nil {
		handlers, err = p.handlersBuilder(ctx, params.chatModel, compressionState)
		if err != nil {
			return nil, err
		}
	}

	runCtx := ctx
	if p.sessionContextBinder != nil {
		runCtx = p.sessionContextBinder(runCtx, params.sessionID)
	}
	if p.toolLifecycleBinder != nil {
		runCtx = p.toolLifecycleBinder(runCtx, params.contextResult.LifecycleState, params.catalog, toolInfos)
	}

	return &assembledTooling{
		allTools:         allTools,
		toolInfos:        toolInfos,
		instruction:      instruction,
		compressionState: compressionState,
		handlers:         handlers,
		runCtx:           runCtx,
	}, nil
}
