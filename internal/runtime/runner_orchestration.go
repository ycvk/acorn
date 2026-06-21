package runtime

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
)

type orchestrationPlane interface {
	BuildDirectResponse(ctx context.Context, req orchestration.DirectResponseRequest) (*orchestration.RunAssembly, error)
}

type defaultOrchestrationPlaneDeps struct {
	cfg          *config.Config
	store        RunnerFactoryStore
	contextPlane contextplane.ContextPlane
	handlers     []adk.ChatModelAgentMiddleware
}

func newDefaultOrchestrationPlane(deps defaultOrchestrationPlaneDeps) *orchestration.DefaultPlane {
	return orchestration.NewDefaultPlane(orchestration.DefaultPlaneOptions{
		SystemPrompt:         deps.cfg.Agent.SystemPrompt,
		MaxIterations:        deps.cfg.Agent.MaxIterations,
		CheckpointStore:      deps.store,
		ToolBuilder:          deps.buildAuditedTools,
		ToolNodeFactory:      deps.buildToolNode,
		HandlersBuilder:      deps.buildHandlers,
		InstructionBuilder:   buildStableInstruction,
		ToolLifecycleBinder:  deps.bindToolLifecycle,
		SessionContextBinder: bindSessionID,
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

// buildRunnerAgentHandlers assembles the chat-model middleware chain. With the
// compaction subpackage removed, compression is driven by the context session
// (see context_session_bridge.go) rather than by model-call middleware; this
// builder now only appends the caller-supplied extra handlers.
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
	_ = ctx
	_ = chatModel
	_ = compressionState
	handlers := make([]adk.ChatModelAgentMiddleware, 0, len(extraHandlers))
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
		return contextplane.WithToolLifecycleContext(ctx, adapter.state, catalog, infos)
	}
	return ctx
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
