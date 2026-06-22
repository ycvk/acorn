package runtime

import (
	"context"
	"errors"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/orchestration"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/toolkit"
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
		CheckpointStore:      newInMemoryCheckpointStore(),
		ToolBuilder:          deps.buildAuditedTools,
		ToolNodeFactory:      deps.buildToolNode,
		HandlersBuilder:      deps.buildHandlers,
		InstructionBuilder:   buildStableInstruction,
		ToolLifecycleBinder:  deps.bindToolLifecycle,
		SessionContextBinder: bindSessionID,
	})
}

// inMemoryCheckpointStore is a process-local adk.CheckPointStore. The schema
// reduction removed the SQLite-backed checkpoints table; runs re-bootstrap
// their context from persisted messages on resume, so a volatile store is
// sufficient for within-process run/resume continuity.
type inMemoryCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *inMemoryCheckpointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	return cp, true, nil
}

func (s *inMemoryCheckpointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(checkPoint))
	copy(cp, checkPoint)
	s.data[checkPointID] = cp
	return nil
}

func (d defaultOrchestrationPlaneDeps) buildAuditedTools(
	ctx context.Context,
	specs []toolkit.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	runID string,
) ([]einotool.BaseTool, error) {
	return tool.BuildAuditedTools(ctx, d.store, specs, excludedToolNames, allowedToolNames, runID)
}

func (d defaultOrchestrationPlaneDeps) buildToolNode(
	ctx context.Context,
	tools []einotool.BaseTool,
	resolver toolkit.ExecutionPolicyResolver,
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
	catalog *toolkit.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	if adapter, ok := state.(toolLifecycleStateAdapter); ok && adapter.state != nil {
		return contextplane.WithToolLifecycleContext(ctx, adapter.state, catalog, infos)
	}
	return ctx
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return domain.WithSessionID(ctx, sessionID)
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
