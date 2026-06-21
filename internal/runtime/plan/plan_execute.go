package plan

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/tooling"
)

type CloseoutNode struct{}

func BuildPlanExecuteGraph(
	ctx context.Context,
	agentName string,
	chatModel einomodel.BaseChatModel,
	maxIterations int,
	checkpointStore compose.CheckPointStore,
	handlers []adk.ChatModelAgentMiddleware,
	planStore runtimeapi.PlanStore,
	planPrompt string,
	eagerToolNames []string,
	toolSpecs []tooling.ToolSpec,
	childExecutor orchestration.ChildAgentExecutor,
) (compose.Runnable[*graph.AgentGraphInput, *schema.Message], error) {
	if err := validateExecuteGraphInputs(chatModel, planStore, childExecutor); err != nil {
		return nil, err
	}
	maxIter := executeMaxIterations(maxIterations)
	g := compose.NewGraph[*graph.AgentGraphInput, *schema.Message](
		compose.WithGenLocalState(executeLocalState(agentName, maxIter)),
	)
	nodes := executeNodeNames()
	if err := addExecuteNodes(ctx, g, nodes, agentName, maxIter, chatModel, handlers, planStore, planPrompt, toolSpecs, childExecutor, eagerToolNames); err != nil {
		return nil, err
	}
	if err := addExecuteEdgesAndBranch(g, nodes); err != nil {
		return nil, err
	}
	return compileExecuteGraph(ctx, g, agentName, checkpointStore)
}

func validateExecuteGraphInputs(chatModel einomodel.BaseChatModel, planStore runtimeapi.PlanStore, childExecutor orchestration.ChildAgentExecutor) error {
	if chatModel == nil {
		return errors.New("plan-execute graph requires a chat model")
	}
	if planStore == nil {
		return errors.New("plan-execute graph requires a plan store")
	}
	if childExecutor == nil {
		return errors.New("plan-execute graph requires a child executor")
	}
	return nil
}

func executeMaxIterations(maxIterations int) int {
	if maxIterations <= 0 {
		return 20
	}
	return maxIterations
}

func executeLocalState(agentName string, maxIter int) func(ctx context.Context) *graph.AgentGraphState {
	return func(ctx context.Context) *graph.AgentGraphState {
		return &graph.AgentGraphState{
			AgentName:           agentName,
			RemainingIterations: maxIter,
		}
	}
}

type executeNodes struct {
	init, plan, executeDispatch, observe, closeout string
}

func executeNodeNames() executeNodes {
	return executeNodes{
		init:            "Init",
		plan:            "Plan",
		executeDispatch: "ExecuteDispatch",
		observe:         "Observe",
		closeout:        "Closeout",
	}
}

func addExecuteNodes(
	ctx context.Context,
	g *compose.Graph[*graph.AgentGraphInput, *schema.Message],
	nodes executeNodes,
	agentName string,
	maxIter int,
	chatModel einomodel.BaseChatModel,
	handlers []adk.ChatModelAgentMiddleware,
	planStore runtimeapi.PlanStore,
	planPrompt string,
	toolSpecs []tooling.ToolSpec,
	childExecutor orchestration.ChildAgentExecutor,
	eagerToolNames []string,
) error {
	wrappedModel, err := wrapExecuteModel(ctx, chatModel, handlers)
	if err != nil {
		return err
	}
	if err := addExecuteInitNode(g, nodes.init, agentName, maxIter); err != nil {
		return err
	}
	if err := addExecutePlanNode(g, nodes.plan, wrappedModel, planStore, planPrompt, toolSpecs, maxIter); err != nil {
		return err
	}
	if err := addExecuteDispatchNode(g, nodes.executeDispatch, planStore, childExecutor); err != nil {
		return err
	}
	if err := addExecuteObserveNode(g, nodes.observe, wrappedModel, planStore); err != nil {
		return err
	}
	return addExecuteCloseoutNode(g, nodes.closeout)
}

func wrapExecuteModel(ctx context.Context, chatModel einomodel.BaseChatModel, handlers []adk.ChatModelAgentMiddleware) (einomodel.BaseChatModel, error) {
	if len(handlers) == 0 {
		return chatModel, nil
	}
	wrappedModel, err := WrapModelWithHandlers(ctx, chatModel, handlers)
	if err != nil {
		return nil, err
	}
	return wrappedModel, nil
}

func addExecuteInitNode(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], name, agentName string, maxIter int) error {
	initLambda := compose.InvokableLambda(func(ctx context.Context, input *graph.AgentGraphInput) (*graph.AgentGraphState, error) {
		return &graph.AgentGraphState{
			Messages:            append([]*schema.Message(nil), input.Messages...),
			RemainingIterations: maxIter,
			AgentName:           agentName,
		}, nil
	})
	if err := g.AddLambdaNode(name, initLambda, compose.WithNodeName(name)); err != nil {
		return fmt.Errorf("add init node: %w", err)
	}
	return nil
}

func addExecutePlanNode(
	g *compose.Graph[*graph.AgentGraphInput, *schema.Message],
	name string,
	wrappedModel einomodel.BaseChatModel,
	planStore runtimeapi.PlanStore,
	planPrompt string,
	toolSpecs []tooling.ToolSpec,
	maxIter int,
) error {
	plan := NewPlanNode(wrappedModel, planStore, planPrompt, enabledPlanToolNamesFromSpecs(toolSpecs))
	lambda := compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		if err := consumePlanIteration(state, maxIter); err != nil {
			return nil, err
		}
		return plan.Invoke(ctx, state)
	})
	if err := g.AddLambdaNode(name, lambda, compose.WithNodeName(name)); err != nil {
		return fmt.Errorf("add plan node: %w", err)
	}
	return nil
}

func addExecuteDispatchNode(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], name string, planStore runtimeapi.PlanStore, childExecutor orchestration.ChildAgentExecutor) error {
	dispatch := NewExecuteDispatchNode(planStore, childExecutor)
	lambda := compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		return dispatch.Invoke(ctx, state)
	})
	if err := g.AddLambdaNode(name, lambda, compose.WithNodeName(name)); err != nil {
		return fmt.Errorf("add execute dispatch node: %w", err)
	}
	return nil
}

func addExecuteObserveNode(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], name string, wrappedModel einomodel.BaseChatModel, planStore runtimeapi.PlanStore) error {
	observe := graph.NewObserveNode(wrappedModel, planStore)
	lambda := compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		decision, err := observe.Decide(ctx, state)
		if err != nil {
			return nil, err
		}
		state.ObserveDecision = decision
		state.Phase = graph.PhaseObserve
		return state, nil
	})
	if err := g.AddLambdaNode(name, lambda, compose.WithNodeName(name)); err != nil {
		return fmt.Errorf("add observe node: %w", err)
	}
	return nil
}

func addExecuteCloseoutNode(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], name string) error {
	closeout := NewCloseoutNode()
	lambda := compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*schema.Message, error) {
		return closeout.Invoke(ctx, state)
	})
	if err := g.AddLambdaNode(name, lambda, compose.WithNodeName(name)); err != nil {
		return fmt.Errorf("add closeout node: %w", err)
	}
	return nil
}

func addExecuteEdgesAndBranch(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], nodes executeNodes) error {
	if err := addExecuteEdges(g, nodes); err != nil {
		return err
	}
	return addExecuteBranch(g, nodes)
}

func addExecuteEdges(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], nodes executeNodes) error {
	edges := [][2]string{
		{compose.START, nodes.init},
		{nodes.init, nodes.plan},
		{nodes.plan, nodes.executeDispatch},
		{nodes.executeDispatch, nodes.observe},
		{nodes.closeout, compose.END},
	}
	for _, edge := range edges {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return fmt.Errorf("add edge %s→%s: %w", edge[0], edge[1], err)
		}
	}
	return nil
}

func addExecuteBranch(g *compose.Graph[*graph.AgentGraphInput, *schema.Message], nodes executeNodes) error {
	branch := compose.NewGraphBranch(func(ctx context.Context, state *graph.AgentGraphState) (string, error) {
		switch state.ObserveDecision.Decision {
		case graph.ObserveDecisionNext:
			return nodes.executeDispatch, nil
		case graph.ObserveDecisionReplan:
			return nodes.plan, nil
		case graph.ObserveDecisionDone:
			return nodes.closeout, nil
		default:
			return "", fmt.Errorf("unknown observe decision %q", state.ObserveDecision.Decision)
		}
	}, map[string]bool{nodes.executeDispatch: true, nodes.plan: true, nodes.closeout: true})
	if err := g.AddBranch(nodes.observe, branch); err != nil {
		return fmt.Errorf("add observe branch: %w", err)
	}
	return nil
}

func compileExecuteGraph(ctx context.Context, g *compose.Graph[*graph.AgentGraphInput, *schema.Message], agentName string, checkpointStore compose.CheckPointStore) (compose.Runnable[*graph.AgentGraphInput, *schema.Message], error) {
	compileOpts := []compose.GraphCompileOption{
		compose.WithGraphName(agentName + "_plan_execute"),
		compose.WithMaxRunSteps(math.MaxInt),
	}
	if !isNilCheckpointStore(checkpointStore) {
		compileOpts = append(compileOpts,
			compose.WithCheckPointStore(checkpointStore),
			compose.WithSerializer(&runtimeapi.JSONSerializer{}),
		)
	}
	runnable, err := g.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, fmt.Errorf("compile plan-execute graph: %w", err)
	}
	return runnable, nil
}
