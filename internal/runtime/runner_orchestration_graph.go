package runtime

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/plan"
)

func BuildRuntimeAgentGraph(ctx context.Context, req orchestration.GraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, err := runtimeGraphDependencies(req.PlanStore)
	if err != nil {
		return nil, err
	}
	runnable, err := plan.BuildAgentGraph(
		ctx, req.AgentName, req.ChatModel, req.SafeToolNode, req.AssistantStreamer,
		req.MaxIterations, req.CheckpointStore, req.Handlers, typedPlanStore,
		req.PlanPrompt, req.EagerToolNames, req.ToolSpecs,
	)
	if err != nil {
		return nil, err
	}
	return wrapGraphAgent(ctx, req.AgentName, req.AgentDescription, runnable, req.ChatModel, req.Tools, req.Handlers, req.MaxIterations, req.CheckpointStore)
}

func BuildRuntimePlanExecuteGraph(ctx context.Context, req orchestration.PlanExecuteGraphBuildRequest) (adk.Agent, error) {
	typedPlanStore, err := runtimeGraphDependencies(req.PlanStore)
	if err != nil {
		return nil, err
	}
	runnable, err := plan.BuildPlanExecuteGraph(
		ctx, req.AgentName, req.ChatModel, req.MaxIterations, req.CheckpointStore,
		req.Handlers, typedPlanStore, req.PlanPrompt, req.EagerToolNames, req.ToolSpecs, req.ChildExecutor,
	)
	if err != nil {
		return nil, err
	}
	return wrapGraphAgent(ctx, req.AgentName, req.AgentDescription, runnable, req.ChatModel, req.Tools, req.Handlers, req.MaxIterations, req.CheckpointStore)
}

func wrapGraphAgent(
	ctx context.Context,
	agentName, agentDescription string,
	runnable compose.Runnable[*graph.AgentGraphInput, *schema.Message],
	chatModel einomodel.BaseChatModel,
	tools []einotool.BaseTool,
	handlers []adk.ChatModelAgentMiddleware,
	maxIterations int,
	checkpointStore adk.CheckPointStore,
) (adk.Agent, error) {
	return graph.NewGraphAgent(
		agentName, agentDescription, runnable, chatModel,
		tools, handlers, maxIterations, checkpointStore,
		graph.GraphAgentContextBinder(ctx),
	), nil
}

func runtimeGraphDependencies(planStore orchestration.PlanStore) (runtimeapi.PlanStore, error) {
	typedPlanStore, ok := planStore.(runtimeapi.PlanStore)
	if !ok {
		return nil, fmt.Errorf("orchestration plane requires runtime plan store")
	}
	return typedPlanStore, nil
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
