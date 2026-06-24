package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/tooldispatch"
	"github.com/ycvk/acorn/internal/tools"
)

// ToolAssembler owns tool assembly for a run: building audited tools from
// catalog specs, binding session/tool-lifecycle context, and assembling the
// instruction + middleware chain. It isolates tool wiring from the
// RunnerFactory so the factory stays a thin coordinator.
type ToolAssembler struct {
	deps RuntimeDeps
}

// NewToolAssembler assembles a ToolAssembler from runtime deps.
func NewToolAssembler(deps RuntimeDeps) *ToolAssembler {
	return &ToolAssembler{deps: deps}
}

// toolAssemblyParams holds the fields BuildDirectResponse shares when
// assembling tools, instruction, handlers, and the bound run context.
type toolAssemblyParams struct {
	catalog           *tools.Catalog
	contextResult     AssembleResultView
	allowedToolNames  []string
	excludedToolNames []string
	runID             string
	chatModel         einomodel.BaseChatModel
	instructionSuffix string
	sessionID         string
}

type assembledTooling struct {
	allTools    []einotool.BaseTool
	toolInfos   []*schema.ToolInfo
	instruction string
	handlers    []adk.ChatModelAgentMiddleware
	runCtx      context.Context
}

// assembleTooling builds the tool set, instruction, handlers, and the run context
// bound with session + tool lifecycle.
func (a *ToolAssembler) assembleTooling(ctx context.Context, params toolAssemblyParams) (*assembledTooling, error) {
	deps := a.deps
	toolBuilder := deps.ToolBuilder
	if toolBuilder == nil {
		toolBuilder = func(ctx context.Context, store RunnerFactoryStore, specs []tools.ToolSpec, excludedToolNames []string, allowedToolNames []string, runID string) ([]einotool.BaseTool, error) {
			return BuildAuditedTools(ctx, store, specs, excludedToolNames, allowedToolNames, runID)
		}
	}
	allTools, err := toolBuilder(ctx, deps.Store, params.catalog.EnabledSpecs(), params.excludedToolNames, params.allowedToolNames, params.runID)
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
	instruction := buildStableInstruction(deps.Config.Agent.SystemPrompt, params.instructionSuffix)
	handlers, err := buildRunnerAgentHandlers(ctx, deps.Config, deps.ContextPlane, deps.Handlers, params.chatModel, nil)
	if err != nil {
		return nil, err
	}
	runCtx := bindSessionID(ctx, params.sessionID)
	runCtx = bindToolLifecycle(runCtx, params.contextResult.LifecycleState, params.catalog, toolInfos)

	return &assembledTooling{
		allTools:    allTools,
		toolInfos:   toolInfos,
		instruction: instruction,
		handlers:    handlers,
		runCtx:      runCtx,
	}, nil
}

func buildDirectResponse(ctx context.Context, deps RuntimeDeps, req DirectResponseRequest, ta *ToolAssembler) (*RunAssembly, error) {
	if deps.Config == nil {
		return nil, fmt.Errorf("runtime deps config is required")
	}
	if req.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}
	if req.AssistantStreamer == nil {
		return nil, fmt.Errorf("assistant streamer is required")
	}
	if req.Catalog == nil {
		return nil, fmt.Errorf("tool catalog is required")
	}
	if req.ContextResult.LifecycleState == nil {
		return nil, fmt.Errorf("context plane lifecycle state is required")
	}

	assembled, err := ta.assembleTooling(ctx, toolAssemblyParams{
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
	toolNodeFactory := deps.ToolNodeFactory
	if toolNodeFactory == nil {
		toolNodeFactory = func(ctx context.Context, tools []einotool.BaseTool, resolver tools.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return tooldispatch.NewSafeParallelToolsNode(ctx, tools, resolver)
		}
	}
	safeToolNode, err := toolNodeFactory(ctx, assembled.allTools, req.Catalog)
	if err != nil {
		return nil, fmt.Errorf("build safe parallel tools node: %w", err)
	}
	agent := &directResponseAgent{
		name:           req.AgentName,
		description:    req.AgentDescription,
		model:          req.ChatModel,
		streamer:       req.AssistantStreamer,
		sessionID:      req.SessionID,
		runID:          req.RunID,
		toolNode:       safeToolNode,
		instruction:    assembled.instruction,
		lifecycleState: req.ContextResult.LifecycleState,
		catalog:        req.Catalog,
		toolInfos:      append([]*schema.ToolInfo(nil), assembled.toolInfos...),
		eagerToolNames: append([]string(nil), req.ContextResult.EagerToolNames...),
		maxIterations:  deps.Config.Agent.MaxIterations,
	}

	return &RunAssembly{
		Runner: adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           agent,
			EnableStreaming: false,
			CheckPointStore: checkpointStore(deps),
		}),
		Instruction: assembled.instruction,
	}, nil
}

type directResponseAgent struct {
	name           string
	description    string
	model          einomodel.BaseChatModel
	streamer       domain.AssistantStreamer
	sessionID      string
	runID          string
	toolNode       tooldispatch.ToolInvoker
	instruction    string
	lifecycleState ToolLifecycleStateView
	catalog        *tools.Catalog
	toolInfos      []*schema.ToolInfo
	eagerToolNames []string
	maxIterations  int
}

type DirectResponseInterruptData struct {
	Iteration int
	Message   *schema.Message
}

func (a *directResponseAgent) Name(context.Context) string {
	return firstNonEmptyString(a.name, "direct_response")
}

func (a *directResponseAgent) Description(context.Context) string {
	return a.description
}

func (a *directResponseAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("direct response panic: %v", r)})
			}
			generator.Close()
		}()
		a.runFromState(ctx, generator, nil)
	}()
	return iterator
}

func (a *directResponseAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("direct response resume panic: %v", r)})
			}
			generator.Close()
		}()
		if info == nil {
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response resume requires interrupt info")})
			return
		}
		resumeData, ok := info.Data.(*DirectResponseInterruptData)
		if !ok || resumeData == nil {
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("direct response resume requires %T interrupt data, got %T", &DirectResponseInterruptData{}, info.Data)})
			return
		}
		a.runFromState(ctx, generator, resumeData)
	}()
	return iterator
}

func (a *directResponseAgent) runFromState(ctx context.Context, generator *adk.AsyncGenerator[*adk.AgentEvent], resumeData *DirectResponseInterruptData) {
	if a.model == nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response agent requires chat model")})
		return
	}
	if a.streamer == nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response agent requires assistant streamer")})
		return
	}
	if a.toolNode == nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response agent requires tool node")})
		return
	}
	if a.maxIterations <= 0 {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response agent requires positive max iterations")})
		return
	}

	runCtx := bindSessionID(ctx, a.sessionID)
	runCtx = bindToolLifecycle(runCtx, a.lifecycleState, a.catalog, a.toolInfos)
	session := contextplane.ContextSessionFromContext(runCtx)
	if session == nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response requires context session")})
		return
	}
	startIteration := 0
	if resumeData != nil {
		if err := a.resumePendingToolCalls(runCtx, session, resumeData); err != nil {
			if signal, ok := errors.AsType[*adk.InterruptSignal](err); ok {
				a.sendInterruptEvent(runCtx, generator, signal, resumeData)
				return
			}
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: err})
			return
		}
		startIteration = resumeData.Iteration + 1
	}

	for iteration := startIteration; iteration < a.maxIterations; iteration++ {
		toolInfos := contextplane.LoadedToolInfosFromContext(runCtx, a.eagerToolNames)
		messageID := directResponseMessageID(a.runID, iteration)
		modelInput, err := session.BeforeModelCall(runCtx, contextplane.ModelCallRequest{
			CallID:    messageID,
			ToolInfos: toolInfos,
		})
		if err != nil {
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("agent loop before model call: %w", err)})
			return
		}
		msg, toolMessages, outputLimitReached, err := ExecuteRound(runCtx, a.model, a.streamer, a.toolNode, modelInput.Messages, toolInfos, a.runID, messageID, RoundOptions{})
		if err == nil {
			if err := session.RecordAssistant(runCtx, msg); err != nil {
				generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("agent loop record assistant: %w", err)})
				return
			}
			if len(toolMessages) > 0 {
				if err := session.RecordToolResults(runCtx, toolMessages); err != nil {
					generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("agent loop record tool results: %w", err)})
					return
				}
			}
			if outputLimitReached {
				if err := session.RecordMessages(runCtx, []adk.Message{outputLimitContinuationMessage()}); err != nil {
					generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("agent loop record output limit continuation: %w", err)})
					return
				}
			}
		}
		if err != nil {
			if signal, ok := errors.AsType[*adk.InterruptSignal](err); ok {
				a.sendInterruptEvent(runCtx, generator, signal, &DirectResponseInterruptData{
					Iteration: iteration,
					Message:   msg,
				})
				return
			}
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: err})
			return
		}
		if msg == nil {
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response chat model returned nil message")})
			return
		}
		generator.Send(adk.EventFromMessage(msg, nil, schema.Assistant, ""))
		if outputLimitReached {
			continue
		}
		if len(msg.ToolCalls) == 0 {
			return
		}
		if len(toolMessages) == 0 {
			generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response tool loop returned no tool messages")})
			return
		}
	}
	generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: fmt.Errorf("direct response tool loop exceeded max iterations %d", a.maxIterations)})
}

func (a *directResponseAgent) resumePendingToolCalls(ctx context.Context, session contextplane.ContextSession, resumeData *DirectResponseInterruptData) error {
	if resumeData == nil {
		return errors.New("direct response resume data is required")
	}
	if resumeData.Message == nil {
		return errors.New("direct response resume message is required")
	}
	if len(resumeData.Message.ToolCalls) == 0 {
		return errors.New("direct response resume message has no pending tool calls")
	}
	toolMessages, err := ExecuteToolCalls(ctx, a.toolNode, resumeData.Message)
	if err != nil {
		return fmt.Errorf("direct response resume get tool results: %w", err)
	}
	if err := session.RecordAssistant(ctx, resumeData.Message); err != nil {
		return fmt.Errorf("direct response resume record assistant: %w", err)
	}
	if len(toolMessages) == 0 {
		return errors.New("direct response resume produced no tool messages")
	}
	if err := session.RecordToolResults(ctx, toolMessages); err != nil {
		return fmt.Errorf("direct response resume record tool results: %w", err)
	}
	return nil
}

func (a *directResponseAgent) sendInterruptEvent(ctx context.Context, generator *adk.AsyncGenerator[*adk.AgentEvent], signal *adk.InterruptSignal, data *DirectResponseInterruptData) {
	event := adk.CompositeInterrupt(ctx, signal.InterruptInfo.Info, signal.InterruptState.State, signal)
	if event.Err != nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: event.Err})
		return
	}
	if event.Action == nil || event.Action.Interrupted == nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response interrupt event missing action")})
		return
	}
	event.AgentName = a.Name(ctx)
	event.Action.Interrupted.Data = data
	generator.Send(event)
}

var _ adk.ResumableAgent = (*directResponseAgent)(nil)

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func directResponseMessageID(runID string, iteration int) string {
	trimmed := strings.TrimSpace(runID)
	if trimmed == "" {
		return fmt.Sprintf("direct_response:assistant:%d", iteration)
	}
	return fmt.Sprintf("%s:assistant:%d", trimmed, iteration)
}
func checkpointStore(deps RuntimeDeps) adk.CheckPointStore {
	if deps.CheckpointStore != nil {
		return deps.CheckpointStore
	}
	return newInMemoryCheckpointStore()
}
