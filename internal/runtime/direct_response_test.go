package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/tooldispatch"
	"github.com/ycvk/acorn/internal/toolkit"
)

func directResponseTestConfig(systemPrompt string, maxIterations int) *config.Config {
	return &config.Config{
		Agent: config.AgentConfig{
			Name:          "test-agent",
			SystemPrompt:  systemPrompt,
			MaxIterations: maxIterations,
		},
	}
}

type directResponseTestModel struct {
	responses    []*schema.Message
	err          error
	streamErrors []error
	inputs       [][]*schema.Message
	toolInfos    [][]*schema.ToolInfo
}

func (m *directResponseTestModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("direct response should use Stream")
}

func (m *directResponseTestModel) Stream(_ context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	m.inputs = append(m.inputs, append([]*schema.Message(nil), input...))
	options := einomodel.GetCommonOptions(nil, opts...)
	m.toolInfos = append(m.toolInfos, append([]*schema.ToolInfo(nil), options.Tools...))
	idx := len(m.inputs) - 1
	if idx < len(m.streamErrors) && m.streamErrors[idx] != nil {
		return nil, m.streamErrors[idx]
	}
	if m.err != nil {
		return nil, m.err
	}
	if idx >= len(m.responses) {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("done", nil)}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{m.responses[idx]}), nil
}

type directResponseTestTool struct {
	name   string
	result string
}

type directResponseTestCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *directResponseTestCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil, false, nil
	}
	value, ok := s.data[id]
	return append([]byte(nil), value...), ok, nil
}

func (s *directResponseTestCheckpointStore) Set(_ context.Context, id string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string][]byte)
	}
	s.data[id] = append([]byte(nil), value...)
	return nil
}

func (t directResponseTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.name,
		Desc: "test tool",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Desc: "query", Type: schema.String, Required: true},
		}),
	}, nil
}

func (t directResponseTestTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return t.result, nil
}

type directResponseTestToolNode struct {
	calls  int
	inputs []*schema.Message
}

type directResponseTestStreamer struct {
	modelInputs [][]*schema.Message
	toolInfos   [][]*schema.ToolInfo
	messageIDs  []string
	deltas      []string
}

func (s *directResponseTestStreamer) StreamAssistantMessage(ctx context.Context, req domain.AssistantStreamRequest) (*domain.AssistantStreamResult, error) {
	s.modelInputs = append(s.modelInputs, append([]*schema.Message(nil), req.Messages...))
	s.toolInfos = append(s.toolInfos, append([]*schema.ToolInfo(nil), req.ToolInfos...))
	s.messageIDs = append(s.messageIDs, req.MessageID)
	stream, err := req.Model.Stream(ctx, req.Messages, einomodel.WithTools(req.ToolInfos))
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, errors.New("stream is nil")
	}
	defer stream.Close()
	var frames []*schema.Message
	for {
		frame, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				break
			}
			return nil, recvErr
		}
		if frame == nil {
			return nil, errors.New("nil frame")
		}
		frames = append(frames, frame)
		if frame.Content != "" {
			s.deltas = append(s.deltas, frame.Content)
		}
	}
	msg, err := schema.ConcatMessages(frames)
	if err != nil {
		return nil, err
	}
	return directResponseTestStreamResult(msg), nil
}

func (s *directResponseTestStreamer) StreamAssistantInterleaved(ctx context.Context, req domain.AssistantStreamRequest) *domain.InterleavedStream {
	interleaved := &domain.InterleavedStream{
		ToolCallCh:     make(chan schema.ToolCall, 8),
		FinalMessageCh: make(chan domain.AssistantStreamResult, 1),
		ErrCh:          make(chan error, 1),
	}
	go func() {
		defer close(interleaved.ToolCallCh)
		defer close(interleaved.FinalMessageCh)
		defer close(interleaved.ErrCh)
		result, err := s.StreamAssistantMessage(ctx, req)
		if err != nil {
			interleaved.ErrCh <- err
			return
		}
		if result == nil || result.Message == nil {
			interleaved.ErrCh <- errors.New("nil assistant stream result")
			return
		}
		for _, tc := range result.Message.ToolCalls {
			interleaved.ToolCallCh <- tc
		}
		interleaved.FinalMessageCh <- *result
	}()
	return interleaved
}

func directResponseTestStreamResult(msg *schema.Message) *domain.AssistantStreamResult {
	raw := ""
	if msg != nil && msg.ResponseMeta != nil {
		raw = strings.TrimSpace(strings.ToLower(msg.ResponseMeta.FinishReason))
	}
	stopReason := domain.AssistantStopReasonEndTurn
	switch raw {
	case "tool_calls", "tool_use":
		stopReason = domain.AssistantStopReasonToolCalls
	case "length", "max_tokens", "max_output_tokens", "model_context_window_exceeded":
		stopReason = domain.AssistantStopReasonMaxOutput
	case "", "stop", "end_turn", "null":
		if msg != nil && len(msg.ToolCalls) > 0 {
			stopReason = domain.AssistantStopReasonToolCalls
		}
	default:
		stopReason = domain.AssistantStopReasonUnknown
	}
	return &domain.AssistantStreamResult{
		Message:    msg,
		StopReason: stopReason,
		RawReason:  raw,
	}
}

func (n *directResponseTestToolNode) Invoke(_ context.Context, input *schema.Message, _ ...compose.ToolsNodeOption) ([]*schema.Message, error) {
	n.calls++
	n.inputs = append(n.inputs, input)
	if input == nil || len(input.ToolCalls) == 0 {
		return nil, errors.New("missing tool call")
	}
	call := input.ToolCalls[0]
	return []*schema.Message{
		schema.ToolMessage("lookup result: acorn", call.ID, schema.WithToolName(call.Function.Name)),
	}, nil
}

func (n *directResponseTestToolNode) Stream(ctx context.Context, input *schema.Message, opts ...compose.ToolsNodeOption) (*schema.StreamReader[[]*schema.Message], error) {
	results, err := n.Invoke(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([][]*schema.Message{results}), nil
}

func (n *directResponseTestToolNode) NewStreamingExecutor(ctx context.Context) tooldispatch.StreamingExecutor {
	return &directResponseTestStreamingExecutor{node: n, ctx: ctx}
}

type directResponseTestStreamingExecutor struct {
	node  *directResponseTestToolNode
	ctx   context.Context
	calls []schema.ToolCall
}

func (e *directResponseTestStreamingExecutor) Submit(call schema.ToolCall) {
	e.calls = append(e.calls, call)
}

func (e *directResponseTestStreamingExecutor) GetRemainingResults(ctx context.Context) ([]*schema.Message, error) {
	if len(e.calls) == 0 {
		return nil, nil
	}
	input := &schema.Message{
		Role:      schema.Assistant,
		ToolCalls: e.calls,
	}
	return e.node.Invoke(ctx, input)
}

func (e *directResponseTestStreamingExecutor) Discard() {}

func TestBuildDirectResponseContinuesAfterOutputLimit(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	model := &directResponseTestModel{
		responses: []*schema.Message{
			directResponseAssistantWithFinishReason("partial answer", nil, "length"),
			schema.AssistantMessage(" final answer", nil),
		},
	}
	streamer := &directResponseTestStreamer{}
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return &directResponseTestToolNode{}, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		AgentDescription:  "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: streamer,
		Catalog:           catalog,
		ContextResult:     contextResult,
		InstructionSuffix: "suffix",
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssembly(t, ctx, assembly)
	if !eventsContainMessage(events, "partial answer") {
		t.Fatalf("direct response events missing partial assistant message: %+v", events)
	}
	if !eventsContainMessage(events, " final answer") {
		t.Fatalf("direct response events missing continuation assistant message: %+v", events)
	}
	if len(model.inputs) != 2 {
		t.Fatalf("model call count = %d, want 2", len(model.inputs))
	}
	if !messagesContainContent(model.inputs[1], "partial answer") {
		t.Fatalf("continuation input missing partial assistant message: %+v", model.inputs[1])
	}
	if !messagesContainContent(model.inputs[1], "Output token limit hit.") {
		t.Fatalf("continuation input missing output-limit continuation marker: %+v", model.inputs[1])
	}
	if len(streamer.messageIDs) != 2 || streamer.messageIDs[0] != "run:assistant:0" || streamer.messageIDs[1] != "run:assistant:1" {
		t.Fatalf("message IDs = %#v, want new assistant id for output-limit continuation", streamer.messageIDs)
	}
}

func TestBuildDirectResponseDoesNotExecuteTruncatedToolCalls(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	model := &directResponseTestModel{
		responses: []*schema.Message{
			directResponseAssistantWithFinishReason("partial tool call", []schema.ToolCall{{
				ID: "call_1",
				Function: schema.FunctionCall{
					Name:      "lookup",
					Arguments: `{"query":"acorn"}`,
				},
			}}, "length"),
			schema.AssistantMessage("continued without tool", nil),
		},
	}
	toolNode := &directResponseTestToolNode{}
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return toolNode, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		AgentDescription:  "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: &directResponseTestStreamer{},
		Catalog:           catalog,
		ContextResult:     contextResult,
		InstructionSuffix: "suffix",
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssembly(t, ctx, assembly)
	if !eventsContainMessage(events, "continued without tool") {
		t.Fatalf("direct response events missing continuation message: %+v", events)
	}
	if toolNode.calls != 0 {
		t.Fatalf("tool node calls = %d, want truncated tool call discarded", toolNode.calls)
	}
	if len(model.inputs) != 2 {
		t.Fatalf("model call count = %d, want 2", len(model.inputs))
	}
	if messagesContainAssistantToolCall(model.inputs[1], "call_1") {
		t.Fatalf("continuation input contains unpaired truncated tool call: %+v", model.inputs[1])
	}
	if messagesContainToolResult(model.inputs[1], "lookup result: acorn") {
		t.Fatalf("continuation input contains tool result for discarded call: %+v", model.inputs[1])
	}
}

func TestBuildDirectResponseRunsToolCallLoop(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	model := &directResponseTestModel{
		responses: []*schema.Message{
			{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{{
					ID: "call_1",
					Function: schema.FunctionCall{
						Name:      "lookup",
						Arguments: `{"query":"acorn"}`,
					},
				}},
			},
			schema.AssistantMessage("lookup result: acorn", nil),
		},
	}
	toolNode := &directResponseTestToolNode{}
	streamer := &directResponseTestStreamer{}
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return toolNode, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		AgentDescription:  "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: streamer,
		Catalog:           catalog,
		ContextResult:     contextResult,
		InstructionSuffix: "suffix",
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssembly(t, ctx, assembly)
	if !eventsContainMessage(events, "lookup result: acorn") {
		t.Fatalf("direct response events missing final assistant message: %+v", events)
	}
	if toolNode.calls != 1 {
		t.Fatalf("tool node calls = %d, want 1", toolNode.calls)
	}
	if len(model.inputs) != 2 {
		t.Fatalf("model call count = %d, want 2", len(model.inputs))
	}
	if len(streamer.deltas) != 1 || streamer.deltas[0] != "lookup result: acorn" {
		t.Fatalf("streamed deltas = %#v, want final content delta", streamer.deltas)
	}
	if len(streamer.messageIDs) != 2 || streamer.messageIDs[0] != "run:assistant:0" || streamer.messageIDs[1] != "run:assistant:1" {
		t.Fatalf("message IDs = %#v, want run-scoped assistant ids", streamer.messageIDs)
	}
	if len(model.toolInfos) != 2 || len(model.toolInfos[0]) != 1 || model.toolInfos[0][0].Name != "lookup" {
		t.Fatalf("first stream tool infos = %#v, want lookup", model.toolInfos)
	}
	if len(model.inputs[0]) == 0 || model.inputs[0][0].Role != schema.System || !strings.Contains(model.inputs[0][0].Content, "system") {
		t.Fatalf("direct response did not prepend stable instruction: %+v", model.inputs[0])
	}
	if messagesContainContent(model.inputs[0], "runner input must not be used") {
		t.Fatalf("direct response used runner input fallback: %+v", model.inputs[0])
	}
	if !messagesContainToolResult(model.inputs[1], "lookup result: acorn") {
		t.Fatalf("second model call missing tool result: %+v", model.inputs[1])
	}
}

func TestBuildDirectResponsePropagatesModelError(t *testing.T) {
	ctx := context.Background()
	modelErr := errors.New("generate failed")
	model := &directResponseTestModel{err: modelErr}
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return &directResponseTestToolNode{}, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: &directResponseTestStreamer{},
		Catalog:           catalog,
		ContextResult:     contextResult,
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssembly(t, ctx, assembly)
	if !eventsContainError(events, modelErr.Error()) {
		t.Fatalf("direct response should expose model error, events=%+v", events)
	}
	if len(model.inputs) != 1 {
		t.Fatalf("model calls = %d, want no retry for non-overflow", len(model.inputs))
	}
}

func TestBuildDirectResponseFailsWithoutContextSession(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return &directResponseTestToolNode{}, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         &directResponseTestModel{responses: []*schema.Message{schema.AssistantMessage("done", nil)}},
		AssistantStreamer: &directResponseTestStreamer{},
		Catalog:           catalog,
		ContextResult:     contextResult,
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssemblyWithoutSession(ctx, assembly)
	if !eventsContainError(events, "direct response requires context session") {
		t.Fatalf("direct response should require context session, events=%+v", events)
	}
}

func directResponseCatalogForTest(t *testing.T, ctx context.Context, tool einotool.BaseTool) *toolkit.Catalog {
	t.Helper()
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	catalog, err := toolkit.NewCatalog(ctx, []toolkit.ToolSpec{{
		ToolContract: toolkit.ToolContract{
			Name:      info.Name,
			Source:    "test",
			Kind:      toolkit.ToolKindNative,
			Category:  toolkit.ToolCategoryRead,
			Loading:   toolkit.EagerLoadingPolicy(),
			Execution: toolkit.ToolExecutionPolicy{ParallelPolicy: toolkit.ParallelPolicyReadOnly},
		},
		Tool: tool,
	}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func directResponseContextResultForTest(runID string, sessionID string, eagerTools ...string) AssembleResultView {
	loaded := make(map[string]contextplane.LoadedToolRecord, len(eagerTools))
	now := time.Now().UTC()
	for _, name := range eagerTools {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		loaded[trimmed] = contextplane.LoadedToolRecord{
			Name:       trimmed,
			LoadedAt:   now,
			LoadSource: "eager",
		}
	}
	state := &contextplane.ToolLifecycleState{
		RunID:         strings.TrimSpace(runID),
		SessionID:     strings.TrimSpace(sessionID),
		LoadedTools:   loaded,
		DeferredTools: map[string]contextplane.DeferredToolRecord{},
		MaxAgeTurns:   2,
	}
	return AssembleResultView{
		LifecycleState: toolLifecycleStateAdapter{state: state},
		EagerToolNames: append([]string(nil), eagerTools...),
	}
}

func messagesContainToolResult(messages []*schema.Message, want string) bool {
	for _, msg := range messages {
		if msg != nil && msg.Role == schema.Tool && strings.Contains(msg.Content, want) {
			return true
		}
	}
	return false
}

func messagesContainContent(messages []*schema.Message, want string) bool {
	for _, msg := range messages {
		if msg != nil && strings.Contains(msg.Content, want) {
			return true
		}
	}
	return false
}

func messagesContainAssistantToolCall(messages []*schema.Message, callID string) bool {
	for _, msg := range messages {
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.ID == callID {
				return true
			}
		}
	}
	return false
}

func directResponseAssistantWithFinishReason(content string, toolCalls []schema.ToolCall, finishReason string) *schema.Message {
	msg := schema.AssistantMessage(content, toolCalls)
	msg.ResponseMeta = &schema.ResponseMeta{FinishReason: finishReason}
	return msg
}

func runDirectResponseAssembly(t *testing.T, ctx context.Context, assembly *RunAssembly) []*adk.AgentEvent {
	t.Helper()
	session := newDirectResponseTestSession(t, ctx, assembly)
	ctx = contextplane.WithContextSession(ctx, session)
	iter := assembly.Runner.Run(ctx, []adk.Message{schema.UserMessage("runner input must not be used")}, adk.WithCheckPointID("run"))
	return collectDirectResponseEvents(iter)
}

func newDirectResponseTestSession(t *testing.T, ctx context.Context, assembly *RunAssembly) contextplane.ContextSession {
	t.Helper()
	counter, err := contextplane.NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter: %v", err)
	}
	session := contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		TokenCounter:        counter,
		WindowTokens:        200000,
		CompactMargin:       13000,
		MaskAfterTurns:      2,
		PreserveRecentTurns: 3,
	})
	initialMessages := []adk.Message{schema.UserMessage("root task")}
	if assembly != nil && strings.TrimSpace(assembly.Instruction) != "" {
		initialMessages = append([]adk.Message{schema.SystemMessage(assembly.Instruction)}, initialMessages...)
	}
	_, err = session.Bootstrap(ctx, contextplane.BootstrapRequest{
		SessionID:       "session",
		RunID:           "run",
		InitialMessages: initialMessages,
	})
	if err != nil {
		t.Fatalf("Bootstrap context session: %v", err)
	}
	return session
}
func runDirectResponseAssemblyWithoutSession(ctx context.Context, assembly *RunAssembly) []*adk.AgentEvent {
	iter := assembly.Runner.Run(ctx, []adk.Message{schema.UserMessage("fallback should fail")}, adk.WithCheckPointID("run"))
	return collectDirectResponseEvents(iter)
}

func collectDirectResponseEvents(iter *adk.AsyncIterator[*adk.AgentEvent]) []*adk.AgentEvent {
	var events []*adk.AgentEvent
	for {
		event, ok := iter.Next()
		if !ok {
			return events
		}
		events = append(events, event)
	}
}

func eventsContainMessage(events []*adk.AgentEvent, want string) bool {
	for _, event := range events {
		if event == nil || event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err == nil && msg != nil && strings.Contains(msg.Content, want) {
			return true
		}
	}
	return false
}

func eventsContainError(events []*adk.AgentEvent, want string) bool {
	for _, event := range events {
		if event != nil && event.Err != nil && strings.Contains(fmt.Sprint(event.Err), want) {
			return true
		}
	}
	return false
}

func eventsContainInterrupted(events []*adk.AgentEvent) bool {
	for _, event := range events {
		if event != nil && event.Action != nil && event.Action.Interrupted != nil {
			return true
		}
	}
	return false
}

func firstInterruptedInfo(events []*adk.AgentEvent) *adk.InterruptInfo {
	for _, event := range events {
		if event != nil && event.Action != nil && event.Action.Interrupted != nil {
			return event.Action.Interrupted
		}
	}
	return nil
}

type directResponseInterruptToolNode struct {
	directResponseTestToolNode
	signal *adk.InterruptSignal
}

func (n *directResponseInterruptToolNode) Invoke(_ context.Context, input *schema.Message, _ ...compose.ToolsNodeOption) ([]*schema.Message, error) {
	if input == nil || len(input.ToolCalls) == 0 {
		return nil, errors.New("missing tool call")
	}
	signal := n.signal
	if signal == nil {
		signal = adk.FromInterruptContexts([]*adk.InterruptCtx{
			{ID: "test-interrupt", Address: adk.Address{}, Info: map[string]any{"kind": "run_command_pause", "command": []string{"ls"}}},
		})
	}
	return nil, signal
}

func (n *directResponseInterruptToolNode) NewStreamingExecutor(ctx context.Context) tooldispatch.StreamingExecutor {
	return &directResponseInterruptStreamingExecutor{node: n, ctx: ctx}
}

type directResponseInterruptStreamingExecutor struct {
	node  *directResponseInterruptToolNode
	ctx   context.Context
	calls []schema.ToolCall
}

func (e *directResponseInterruptStreamingExecutor) Submit(call schema.ToolCall) {
	e.calls = append(e.calls, call)
}

func (e *directResponseInterruptStreamingExecutor) GetRemainingResults(ctx context.Context) ([]*schema.Message, error) {
	if len(e.calls) == 0 {
		return nil, nil
	}
	input := &schema.Message{
		Role:      schema.Assistant,
		ToolCalls: e.calls,
	}
	return e.node.Invoke(ctx, input)
}

func (e *directResponseInterruptStreamingExecutor) Discard() {}

func TestBuildDirectResponseHandlesInterrupt(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	model := &directResponseTestModel{
		responses: []*schema.Message{
			{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{{
					ID: "call_1",
					Function: schema.FunctionCall{
						Name:      "lookup",
						Arguments: `{"query":"acorn"}`,
					},
				}},
			},
		},
	}
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return &directResponseInterruptToolNode{}, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: &directResponseTestStreamer{},
		Catalog:           catalog,
		ContextResult:     contextResult,
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssembly(t, ctx, assembly)
	if !eventsContainInterrupted(events) {
		t.Fatalf("expected run to be interrupted, got events: %+v", events)
	}
	if eventsContainError(events, "Run failed") {
		t.Fatal("run should not be marked as failed when an interrupt is detected")
	}
}

func TestBuildDirectResponsePreservesNestedInterruptContexts(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "lookup result: acorn"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	model := &directResponseTestModel{
		responses: []*schema.Message{
			{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{{
					ID: "call_1",
					Function: schema.FunctionCall{
						Name:      "lookup",
						Arguments: `{"query":"acorn"}`,
					},
				}},
			},
		},
	}
	interruptSignal := &adk.InterruptSignal{ID: "root-signal", Address: adk.Address{}}
	interruptSignal.Info = map[string]any{"kind": "root_wrapper"}
	interruptSignal.IsRootCause = false
	ctx1 := &adk.InterruptSignal{ID: "ctx_1", Address: adk.Address{}}
	ctx1.Info = map[string]any{"kind": "run_command_pause", "command": []string{"git", "status"}}
	ctx1.IsRootCause = true
	ctx2 := &adk.InterruptSignal{ID: "ctx_2", Address: adk.Address{}}
	ctx2.Info = map[string]any{"kind": "run_command_pause"}
	ctx2.IsRootCause = false
	ctx21 := &adk.InterruptSignal{ID: "ctx_2_1", Address: adk.Address{}}
	ctx21.Info = map[string]any{"kind": "nested_pause_child"}
	ctx21.IsRootCause = false
	ctx2.Subs = []*adk.InterruptSignal{ctx21}
	interruptSignal.Subs = []*adk.InterruptSignal{ctx1, ctx2}
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: &directResponseTestCheckpointStore{},
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return &directResponseInterruptToolNode{signal: interruptSignal}, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: &directResponseTestStreamer{},
		Catalog:           catalog,
		ContextResult:     contextResult,
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	events := runDirectResponseAssembly(t, ctx, assembly)
	info := firstInterruptedInfo(events)
	if info == nil {
		t.Fatalf("expected interrupted info, got events: %+v", events)
	}
	if got, want := len(info.InterruptContexts), 1; got != want {
		t.Fatalf("interrupt context count = %d, want %d", got, want)
	}
	if info.Data == nil {
		t.Fatal("interrupt info data should preserve pending direct response state")
	}
	got := info.InterruptContexts[0]
	if got.ID != "ctx_1" || got.IsRootCause != true {
		t.Fatalf("root interrupt context = %+v", got)
	}
	if got.Parent == nil {
		t.Fatalf("interrupt parent chain = %+v", got.Parent)
	}
	parentInfo, _ := got.Parent.Info.(map[string]any)
	if parentInfo["kind"] != "root_wrapper" {
		t.Fatalf("interrupt parent chain = %+v", got.Parent)
	}
}

type directResponseApprovalToolNode struct {
	calls int
}

func (n *directResponseApprovalToolNode) Invoke(ctx context.Context, input *schema.Message, _ ...compose.ToolsNodeOption) ([]*schema.Message, error) {
	if input == nil || len(input.ToolCalls) == 0 {
		return nil, errors.New("missing tool call")
	}
	call := input.ToolCalls[0]
	n.calls++
	wasInterrupted, _, _ := einotool.GetInterruptState[string](ctx)
	if !wasInterrupted {
		info := map[string]any{
			"kind":    "run_command_pause",
			"command": []string{"./acorn", "--help"},
			"cwd":     "/Users/ycvk/GolandProjects/acorn",
		}
		return nil, einotool.StatefulInterrupt(ctx, info, "pending-command")
	}
	return []*schema.Message{
		schema.ToolMessage("approved", call.ID, schema.WithToolName(call.Function.Name)),
	}, nil
}

func (n *directResponseApprovalToolNode) NewStreamingExecutor(ctx context.Context) tooldispatch.StreamingExecutor {
	return &directResponseApprovalStreamingExecutor{node: n, ctx: ctx}
}

type directResponseApprovalStreamingExecutor struct {
	node  *directResponseApprovalToolNode
	ctx   context.Context
	calls []schema.ToolCall
}

func (e *directResponseApprovalStreamingExecutor) Submit(call schema.ToolCall) {
	e.calls = append(e.calls, call)
}

func (e *directResponseApprovalStreamingExecutor) GetRemainingResults(ctx context.Context) ([]*schema.Message, error) {
	if len(e.calls) == 0 {
		return nil, nil
	}
	input := &schema.Message{
		Role:      schema.Assistant,
		ToolCalls: e.calls,
	}
	return e.node.Invoke(ctx, input)
}

func (e *directResponseApprovalStreamingExecutor) Discard() {}

func TestBuildDirectResponseResumeContinuesFromPendingToolCalls(t *testing.T) {
	ctx := context.Background()
	tool := directResponseTestTool{name: "lookup", result: "unused"}
	catalog := directResponseCatalogForTest(t, ctx, tool)
	contextResult := directResponseContextResultForTest("run", "session", "lookup")
	model := &directResponseTestModel{
		responses: []*schema.Message{
			{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{{
					ID: "call_1",
					Function: schema.FunctionCall{
						Name:      "lookup",
						Arguments: `{"query":"acorn"}`,
					},
				}},
			},
			schema.AssistantMessage("done", nil),
		},
	}
	toolNode := &directResponseApprovalToolNode{}
	store := &directResponseTestCheckpointStore{}
	deps := RuntimeDeps{
		Config:          directResponseTestConfig("system", 4),
		CheckpointStore: store,
		ToolBuilder: func(context.Context, RunnerFactoryStore, []toolkit.ToolSpec, []string, []string, string) ([]einotool.BaseTool, error) {
			return []einotool.BaseTool{tool}, nil
		},
		ToolNodeFactory: func(context.Context, []einotool.BaseTool, toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error) {
			return toolNode, nil
		},
	}
	assembly, err := buildDirectResponse(ctx, deps, DirectResponseRequest{
		AgentName:         "agent",
		SessionID:         "session",
		RunID:             "run",
		ChatModel:         model,
		AssistantStreamer: &directResponseTestStreamer{},
		Catalog:           catalog,
		ContextResult:     contextResult,
	})
	if err != nil {
		t.Fatalf("BuildDirectResponse: %v", err)
	}

	session := newDirectResponseTestSession(t, ctx, assembly)
	runCtx := contextplane.WithContextSession(ctx, session)
	initial := collectDirectResponseEvents(assembly.Runner.Run(runCtx, []adk.Message{schema.UserMessage("runner input must not be used")}, adk.WithCheckPointID("run")))
	info := firstInterruptedInfo(initial)
	if info == nil || len(info.InterruptContexts) != 1 {
		t.Fatalf("expected single interrupted context, got %+v", info)
	}
	if _, ok, err := store.Get(ctx, "run"); err != nil || !ok {
		var eventSummary []string
		for _, event := range initial {
			switch {
			case event == nil:
				eventSummary = append(eventSummary, "<nil>")
			case event.Err != nil:
				eventSummary = append(eventSummary, "err:"+event.Err.Error())
			case event.Action != nil && event.Action.Interrupted != nil:
				eventSummary = append(eventSummary, "interrupt")
			case event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil:
				eventSummary = append(eventSummary, "message:"+event.Output.MessageOutput.Message.Content)
			default:
				eventSummary = append(eventSummary, "other")
			}
		}
		t.Fatalf("expected runner checkpoint to be persisted, err=%v events=%v", err, eventSummary)
	}

	resumeCtx := contextplane.WithContextSession(context.Background(), session)
	iter, err := assembly.Runner.ResumeWithParams(resumeCtx, "run", &adk.ResumeParams{
		Targets: map[string]any{
			info.InterruptContexts[0].ID: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("ResumeWithParams: %v", err)
	}
	resumed := collectDirectResponseEvents(iter)
	if eventsContainError(resumed, "failed") {
		t.Fatalf("resume events contain error: %+v", resumed)
	}
	if got, want := len(model.inputs), 2; got != want {
		var eventSummary []string
		for _, event := range resumed {
			switch {
			case event == nil:
				eventSummary = append(eventSummary, "<nil>")
			case event.Err != nil:
				eventSummary = append(eventSummary, "err:"+event.Err.Error())
			case event.Action != nil && event.Action.Interrupted != nil:
				eventSummary = append(eventSummary, "interrupt")
			case event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil:
				eventSummary = append(eventSummary, "message:"+event.Output.MessageOutput.Message.Content)
			default:
				eventSummary = append(eventSummary, "other")
			}
		}
		t.Fatalf("model stream count = %d, want %d, resumed=%v", got, want, eventSummary)
	}
	if got, want := toolNode.calls, 2; got != want {
		t.Fatalf("tool call attempts = %d, want %d", got, want)
	}
	last := resumed[len(resumed)-1]
	if last == nil || last.Output == nil || last.Output.MessageOutput == nil || last.Output.MessageOutput.Message == nil {
		t.Fatalf("unexpected resumed events: %+v", resumed)
	}
	if got := last.Output.MessageOutput.Message.Content; got != "done" {
		t.Fatalf("final resumed content = %q, want done", got)
	}
}
