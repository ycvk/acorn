package orchestration

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
	"github.com/ycvk/acorn/internal/tooling"
)

// toolAssemblyParams holds the fields BuildDirectResponse shares when
// assembling tools, instruction, handlers, and the bound run context.
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
// bound with session + tool lifecycle.
func (p *DefaultPlane) assembleTooling(ctx context.Context, params toolAssemblyParams) (*assembledTooling, error) {
	allTools, err := p.toolBuilder(
		ctx,
		params.catalog.EnabledSpecs(),
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
	handlers, err := p.handlersBuilder(ctx, params.chatModel, compressionState)
	if err != nil {
		return nil, err
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

func (p *DefaultPlane) BuildDirectResponse(ctx context.Context, req DirectResponseRequest) (*RunAssembly, error) {
	if p == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
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
	if p.toolBuilder == nil || p.toolNodeFactory == nil || p.instructionBuilder == nil || p.handlersBuilder == nil {
		return nil, fmt.Errorf("orchestration plane is missing required dependencies")
	}
	if p.checkpointStore == nil {
		return nil, fmt.Errorf("orchestration plane requires checkpoint store")
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
	agent := &directResponseAgent{
		name:                 req.AgentName,
		description:          req.AgentDescription,
		model:                req.ChatModel,
		streamer:             req.AssistantStreamer,
		sessionID:            req.SessionID,
		runID:                req.RunID,
		toolNode:             safeToolNode,
		instruction:          assembled.instruction,
		sessionContextBinder: p.sessionContextBinder,
		lifecycleBinder:      p.toolLifecycleBinder,
		lifecycleState:       req.ContextResult.LifecycleState,
		catalog:              req.Catalog,
		toolInfos:            append([]*schema.ToolInfo(nil), assembled.toolInfos...),
		eagerToolNames:       append([]string(nil), req.ContextResult.EagerToolNames...),
		maxIterations:        p.maxIterations,
	}

	return &RunAssembly{
		Runner: adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           agent,
			EnableStreaming: false,
			CheckPointStore: p.checkpointStore,
		}),
		Instruction:      assembled.instruction,
		CompressionState: assembled.compressionState,
	}, nil
}

type directResponseAgent struct {
	name                 string
	description          string
	model                einomodel.BaseChatModel
	streamer             AssistantStreamer
	sessionID            string
	runID                string
	toolNode             ToolInvoker
	instruction          string
	sessionContextBinder func(ctx context.Context, sessionID string) context.Context
	lifecycleBinder      ToolLifecycleBinder
	lifecycleState       ToolLifecycleStateView
	catalog              *tooling.Catalog
	toolInfos            []*schema.ToolInfo
	eagerToolNames       []string
	maxIterations        int
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

	runCtx := ctx
	if a.sessionContextBinder != nil {
		runCtx = a.sessionContextBinder(runCtx, a.sessionID)
	}
	if a.lifecycleBinder != nil {
		runCtx = a.lifecycleBinder(runCtx, a.lifecycleState, a.catalog, a.toolInfos)
	}
	session := contextplane.ContextSessionFromContext(runCtx)
	if session == nil {
		generator.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: errors.New("direct response requires context session")})
		return
	}

	loop := NewAgentLoop(a.model, a.toolNode, a.streamer, session)
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
		result, err := loop.RunOneIteration(runCtx, toolInfos, a.runID, directResponseMessageID(a.runID, iteration), true)
		var msg *schema.Message
		if result != nil {
			msg = result.Message
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
		if result.OutputLimitReached {
			continue
		}
		if len(msg.ToolCalls) == 0 {
			return
		}
		if len(result.ToolMessages) == 0 {
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
