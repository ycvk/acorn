package plan

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/tooling"
)

func BuildAgentGraph(
	ctx context.Context,
	agentName string,
	chatModel einomodel.BaseChatModel,
	safeToolNode orchestration.ToolInvoker,
	streamer orchestration.AssistantStreamer,
	maxIterations int,
	checkpointStore compose.CheckPointStore,
	handlers []adk.ChatModelAgentMiddleware,
	planStore runtimeapi.PlanStore,
	planPrompt string,
	planningPromptProvider PlanningPromptProvider,
	eagerToolNames []string,
	toolSpecs []tooling.ToolSpec,
) (compose.Runnable[*graph.AgentGraphInput, *schema.Message], error) {
	if chatModel == nil {
		return nil, errors.New("agent graph requires a chat model")
	}
	if safeToolNode == nil {
		return nil, errors.New("agent graph requires a safe tool node")
	}
	if planStore == nil {
		return nil, errors.New("agent graph requires a plan store")
	}

	const (
		initNode    = "Init"
		planNode    = "Plan"
		actNode     = "Act"
		observeNode = "Observe"
		finalNode   = "Final"
	)

	maxIter := maxIterations
	if maxIter <= 0 {
		maxIter = 20
	}

	g := compose.NewGraph[*graph.AgentGraphInput, *schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *graph.AgentGraphState {
			return &graph.AgentGraphState{
				AgentName:           agentName,
				RemainingIterations: maxIter,
			}
		}),
	)

	initLambda := compose.InvokableLambda(func(ctx context.Context, input *graph.AgentGraphInput) (*graph.AgentGraphState, error) {
		state := &graph.AgentGraphState{
			Messages:            append([]*schema.Message(nil), input.Messages...),
			RemainingIterations: maxIter,
			AgentName:           agentName,
		}
		return state, nil
	})
	if err := g.AddLambdaNode(initNode, initLambda, compose.WithNodeName(initNode)); err != nil {
		return nil, fmt.Errorf("add init node: %w", err)
	}

	wrappedModel := chatModel
	if len(handlers) > 0 {
		var err error
		wrappedModel, err = WrapModelWithHandlers(ctx, chatModel, handlers)
		if err != nil {
			return nil, err
		}
	}
	eventStore := eventAppenderFromCheckpointStore(checkpointStore)
	plan := NewPlanNode(wrappedModel, planStore, eventStore, planPrompt, planningPromptProvider, enabledPlanToolNamesFromSpecs(toolSpecs))
	act := NewActNode(wrappedModel, safeToolNode, streamer, planStore, eventStore, toolSpecs, eagerToolNames)
	observe := graph.NewObserveNode(wrappedModel, planStore)

	if err := g.AddLambdaNode(planNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		if err := consumePlanIteration(state, maxIter); err != nil {
			return nil, err
		}
		return plan.Invoke(ctx, state)
	}), compose.WithNodeName(planNode)); err != nil {
		return nil, fmt.Errorf("add plan node: %w", err)
	}

	if err := g.AddLambdaNode(actNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		return act.Invoke(ctx, state)
	}), compose.WithNodeName(actNode)); err != nil {
		return nil, fmt.Errorf("add act node: %w", err)
	}

	if err := g.AddLambdaNode(observeNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*graph.AgentGraphState, error) {
		decision, err := observe.Decide(ctx, state)
		if err != nil {
			return nil, err
		}
		state.ObserveDecision = decision
		state.Phase = graph.PhaseObserve
		return state, nil
	}), compose.WithNodeName(observeNode)); err != nil {
		return nil, fmt.Errorf("add observe node: %w", err)
	}

	if err := g.AddLambdaNode(finalNode, compose.InvokableLambda(func(ctx context.Context, state *graph.AgentGraphState) (*schema.Message, error) {
		return finalMessageFromGraphState(state), nil
	}), compose.WithNodeName(finalNode)); err != nil {
		return nil, fmt.Errorf("add final node: %w", err)
	}

	if err := g.AddEdge(compose.START, initNode); err != nil {
		return nil, fmt.Errorf("add start→init edge: %w", err)
	}
	if err := g.AddEdge(initNode, planNode); err != nil {
		return nil, fmt.Errorf("add init→plan edge: %w", err)
	}
	if err := g.AddEdge(planNode, actNode); err != nil {
		return nil, fmt.Errorf("add plan→act edge: %w", err)
	}
	if err := g.AddEdge(actNode, observeNode); err != nil {
		return nil, fmt.Errorf("add act→observe edge: %w", err)
	}

	observeBranch := compose.NewGraphBranch(func(ctx context.Context, state *graph.AgentGraphState) (string, error) {
		switch state.ObserveDecision.Decision {
		case graph.ObserveDecisionNext:
			return actNode, nil
		case graph.ObserveDecisionReplan:
			return planNode, nil
		case graph.ObserveDecisionDone:
			return finalNode, nil
		default:
			return "", fmt.Errorf("unknown observe decision %q", state.ObserveDecision.Decision)
		}
	}, map[string]bool{actNode: true, planNode: true, finalNode: true})
	if err := g.AddBranch(observeNode, observeBranch); err != nil {
		return nil, fmt.Errorf("add observe branch: %w", err)
	}
	if err := g.AddEdge(finalNode, compose.END); err != nil {
		return nil, fmt.Errorf("add final→end edge: %w", err)
	}

	compileOpts := []compose.GraphCompileOption{
		compose.WithGraphName(agentName),
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
		return nil, fmt.Errorf("compile agent graph: %w", err)
	}

	return runnable, nil
}

func eventAppenderFromCheckpointStore(store compose.CheckPointStore) runtimeapi.EventAppender {
	if isNilCheckpointStore(store) {
		return nil
	}
	appender, ok := store.(runtimeapi.EventAppender)
	if !ok {
		return nil
	}
	return appender
}

func enabledPlanToolNamesFromSpecs(specs []tooling.ToolSpec) []string {
	names := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" || !spec.Enabled() || !spec.HasProfile(tooling.ToolProfileRun) {
			continue
		}
		names[name] = struct{}{}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func isNilCheckpointStore(store compose.CheckPointStore) bool {
	if store == nil {
		return true
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func consumePlanIteration(state *graph.AgentGraphState, maxIter int) error {
	if state.RemainingIterations <= 0 {
		return fmt.Errorf("exceeds max iterations (%d)", maxIter)
	}
	state.RemainingIterations--
	return nil
}

func finalMessageFromGraphState(state *graph.AgentGraphState) *schema.Message {
	if state == nil {
		return schema.AssistantMessage("", nil)
	}
	for i := len(state.Messages) - 1; i >= 0; i-- {
		msg := state.Messages[i]
		if msg == nil || stringsTrim(msg.Content) == "" {
			continue
		}
		if msg.Role == schema.Assistant {
			if isGraphControlMessage(msg.Content) {
				continue
			}
			return msg
		}
		if msg.Role == schema.Tool {
			return schema.AssistantMessage(msg.Content, nil)
		}
	}
	if state.Plan != nil {
		return schema.AssistantMessage(graph.FormatPlanSummary(state.Plan), nil)
	}
	return schema.AssistantMessage("", nil)
}

func isGraphControlMessage(content string) bool {
	if _, err := graph.ParseObserveDecision(content); err == nil {
		return true
	}
	if _, err := parsePlanSteps(content); err == nil {
		return true
	}
	return false
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}

func WrapModelWithHandlers(ctx context.Context, model einomodel.BaseChatModel, handlers []adk.ChatModelAgentMiddleware) (einomodel.BaseChatModel, error) {
	wrapped := model
	for _, h := range handlers {
		mc := &adk.ModelContext{}
		newModel, err := h.WrapModel(ctx, wrapped, mc)
		if err != nil {
			return nil, fmt.Errorf("wrap chat model middleware: %w", err)
		}
		if newModel != nil {
			wrapped = newModel
		}
	}
	return wrapped, nil
}
