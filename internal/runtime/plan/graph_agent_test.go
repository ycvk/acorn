package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	rtool "github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/runtime/tooltest"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/tooling"
)

type stubChatModel struct {
	responses []string
	callCount int
}

func (m *stubChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	idx := m.callCount
	m.callCount++
	if idx >= len(m.responses) {
		return schema.AssistantMessage("done", nil), nil
	}
	return schema.AssistantMessage(m.responses[idx], nil), nil
}

func (m *stubChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type stubTool struct {
	name        string
	description string
	result      string
	shouldErr   bool
}

func (t *stubTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.name,
		Desc: t.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Desc: "query", Type: schema.String, Required: true},
		}),
	}, nil
}

func (t *stubTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t.shouldErr {
		return "", fmt.Errorf("stub tool %s error", t.name)
	}
	return t.result, nil
}

type toolCallingStubModel struct {
	responses []*schema.Message
	callCount int
}

func (m *toolCallingStubModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	idx := m.callCount
	m.callCount++
	if idx >= len(m.responses) {
		return schema.AssistantMessage("done", nil), nil
	}
	return m.responses[idx], nil
}

func (m *toolCallingStubModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *toolCallingStubModel) WithTools(_ []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

type recordingGraphRunnable struct {
	checkpointIDs []string
}

func (r *recordingGraphRunnable) Invoke(_ context.Context, _ *graph.AgentGraphInput, opts ...compose.Option) (*schema.Message, error) {
	r.checkpointIDs = append(r.checkpointIDs, checkpointIDFromComposeOptions(opts))
	return schema.AssistantMessage("ok", nil), nil
}

func (r *recordingGraphRunnable) Stream(context.Context, *graph.AgentGraphInput, ...compose.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("recording runnable Stream is not used")
}

func (r *recordingGraphRunnable) Collect(context.Context, *schema.StreamReader[*graph.AgentGraphInput], ...compose.Option) (*schema.Message, error) {
	return nil, errors.New("recording runnable Collect is not used")
}

func (r *recordingGraphRunnable) Transform(context.Context, *schema.StreamReader[*graph.AgentGraphInput], ...compose.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("recording runnable Transform is not used")
}

type noopCheckPointStore struct{}

func (noopCheckPointStore) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (noopCheckPointStore) Set(context.Context, string, []byte) error         { return nil }

func checkpointIDFromComposeOptions(opts []compose.Option) string {
	for _, opt := range opts {
		if id := checkpointIDFromComposeOption(opt); id != "" {
			return id
		}
	}
	return ""
}

func checkpointIDFromComposeOption(opt compose.Option) string {
	value := reflect.ValueOf(opt)
	field := value.FieldByName("checkPointID")
	if !field.IsValid() || field.Kind() != reflect.Ptr || field.IsNil() {
		return ""
	}
	elem := field.Elem()
	if elem.Kind() != reflect.String {
		return ""
	}
	return elem.String()
}

func buildTestAgentGraph(
	t *testing.T,
	ctx context.Context,
	model einomodel.BaseChatModel,
	safeNode *rtool.SafeParallelToolsNode,
	maxIter int,
	store *storesqlite.Store,
	toolInfos []*schema.ToolInfo,
	toolSpecs []tooling.ToolSpec,
) compose.Runnable[*graph.AgentGraphInput, *schema.Message] {
	t.Helper()
	planStore := runtimeapi.PlanStore(&fakePlanStore{})
	if store != nil {
		planStore = NewPlanStore(store)
	}
	eagerToolNames := make([]string, 0, len(toolInfos))
	for _, info := range toolInfos {
		if info != nil {
			eagerToolNames = append(eagerToolNames, info.Name)
		}
	}
	runnable, err := BuildAgentGraph(ctx, "test-agent", model, safeNode, rtool.NewDirectAssistantStreamer(nil), maxIter, store, nil, planStore, "Make a plan", eagerToolNames, toolSpecs)
	if err != nil {
		t.Fatalf("buildAgentGraph: %v", err)
	}
	return runnable
}

func withGraphTestContext(ctx context.Context) context.Context {
	ctx = runtimeapi.WithSessionID(ctx, "sess_graph")
	ctx = runtimeapi.WithRunID(ctx, "run_graph")
	return ctx
}

func TestJSONSerializerRoundTrip(t *testing.T) {
	msg := schema.UserMessage("hello")
	wrapper := &schemaMessageWrapper{
		Type:  "*schema.Message",
		Value: msg,
	}
	data, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded schemaMessageWrapper
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Value == nil {
		t.Fatal("Value is nil after round-trip")
	}
	if decoded.Value.Content != "hello" {
		t.Errorf("Content mismatch: got %q, want %q", decoded.Value.Content, "hello")
	}
	if decoded.Type != "*schema.Message" {
		t.Errorf("Type mismatch: got %q", decoded.Type)
	}
}

func TestBuildAgentGraphWithSafeToolNode(t *testing.T) {
	ctx := withGraphTestContext(context.Background())
	model := &stubChatModel{responses: []string{`{"steps":[{"id":"s1","action":"say hello","status":"pending"}]}`, "hello", `{"decision":"done"}`}}
	tools := []einotool.BaseTool{
		&stubTool{name: "search", description: "search things", result: "found"},
	}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	runnable := buildTestAgentGraph(t, ctx, model, safeNode, 10, nil, nil, nil)
	if runnable == nil {
		t.Fatal("buildAgentGraph returned nil runnable")
	}
}

func TestAgentGraphPlanActObserveRun(t *testing.T) {
	ctx := withGraphTestContext(context.Background())
	model := &stubChatModel{responses: []string{
		`{"steps":[{"id":"s1","action":"answer greeting","status":"pending"}]}`,
		"Hello from plan loop.",
		`{"decision":"done"}`,
	}}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), nil, fixedReadOnlyClassifier())
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	runnable := buildTestAgentGraph(t, ctx, model, safeNode, 10, nil, nil, nil)

	msg, err := runnable.Invoke(ctx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("hi")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if msg.Content != "Hello from plan loop." {
		t.Fatalf("content = %q, want plan loop response", msg.Content)
	}
}

func TestGraphAgentRunNoTools(t *testing.T) {
	ctx := withGraphTestContext(context.Background())
	model := &stubChatModel{responses: []string{`{"steps":[{"id":"s1","action":"answer greeting","status":"pending"}]}`, "Hello! I can help you.", `{"decision":"done"}`}}
	tools := []einotool.BaseTool{}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier())
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	runnable := buildTestAgentGraph(t, ctx, model, safeNode, 10, nil, nil, nil)
	agent := graph.NewGraphAgent("test-agent", "test", runnable, model, tools, nil, 10, nil, nil)

	input := &adk.AgentInput{
		Messages: []*schema.Message{schema.UserMessage("hi")},
	}
	iter := agent.Run(ctx, input)

	event, ok := iter.Next()
	if !ok {
		t.Fatal("expected event from Run")
	}
	if event.Err != nil {
		t.Fatalf("unexpected error: %v", event.Err)
	}
	if event.Output == nil || event.Output.MessageOutput == nil || event.Output.MessageOutput.Message == nil {
		t.Fatal("expected message output")
	}
	if event.Output.MessageOutput.Message.Content != "Hello! I can help you." {
		t.Errorf("unexpected content: got %q", event.Output.MessageOutput.Message.Content)
	}
}

func TestGraphAgentRunWithToolCall(t *testing.T) {
	ctx := withGraphTestContext(context.Background())

	toolCallJSON, _ := json.Marshal(map[string]any{"query": "test query"})
	toolCallMsg := &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      "search",
					Arguments: string(toolCallJSON),
				},
			},
		},
	}

	model2 := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"search for test","status":"pending"}]}`, nil),
			toolCallMsg,
			schema.AssistantMessage(`{"decision":"done"}`, nil),
		},
	}

	tools := []einotool.BaseTool{
		&stubTool{name: "search", description: "search things", result: "found"},
	}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	ctx = safeParallelLifecycleContextFrom(t, ctx, tools)
	info, err := tools[0].Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable := buildTestAgentGraph(t, ctx, model2, safeNode, 10, nil, []*schema.ToolInfo{info}, nil)
	agent := graph.NewGraphAgent("test-agent", "test", runnable, model2, tools, nil, 10, nil, graph.GraphAgentContextBinder(ctx))

	input := &adk.AgentInput{
		Messages: []*schema.Message{schema.UserMessage("search for test")},
	}
	iter := agent.Run(withGraphTestContext(context.Background()), input)

	var lastContent string
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("unexpected error: %v", event.Err)
		}
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			lastContent = event.Output.MessageOutput.Message.Content
		}
	}
	if !strings.Contains(lastContent, "found") {
		t.Errorf("expected output to contain 'found', got: %q", lastContent)
	}
}

func TestGraphAgentMaxIterations(t *testing.T) {
	ctx := withGraphTestContext(context.Background())

	model := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"loop","status":"pending"},{"id":"s2","action":"continue loop","status":"pending","depends_on":["s1"]}]}`, nil),
			makeAssistantMessage(makeToolCall("call_1", "search", `{"query":"loop"}`)),
			schema.AssistantMessage(`{"decision":"replan"}`, nil),
		},
	}

	tools := []einotool.BaseTool{
		&stubTool{name: "search", description: "search things", result: "result"},
	}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	ctx = safeParallelLifecycleContextFrom(t, ctx, tools)
	info, err := tools[0].Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable := buildTestAgentGraph(t, ctx, model, safeNode, 1, nil, []*schema.ToolInfo{info}, nil)
	agent := graph.NewGraphAgent("test-agent", "test", runnable, model, tools, nil, 3, nil, graph.GraphAgentContextBinder(ctx))

	input := &adk.AgentInput{
		Messages: []*schema.Message{schema.UserMessage("loop forever")},
	}
	iter := agent.Run(ctx, input)

	var gotErr bool
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil && strings.Contains(event.Err.Error(), "max iterations") {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("expected max iterations error")
	}
}

func TestGraphAgentRunAndResumeUseScopedComposeCheckpoint(t *testing.T) {
	ctx := runtimeapi.WithRunID(context.Background(), "run_graph_checkpoint")
	runnable := &recordingGraphRunnable{}
	agent := graph.NewGraphAgent("test-agent", "test", runnable, nil, nil, nil, 10, noopCheckPointStore{}, nil)

	runIter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("start")}})
	assertGraphAgentIteratorClean(t, runIter)

	resumeIter := agent.Resume(ctx, &adk.ResumeInfo{InterruptInfo: &adk.InterruptInfo{Data: "not-a-checkpoint-id"}})
	assertGraphAgentIteratorClean(t, resumeIter)

	want := "graph:run_graph_checkpoint:test-agent"
	if len(runnable.checkpointIDs) != 2 {
		t.Fatalf("checkpoint ids = %v, want two invocations", runnable.checkpointIDs)
	}
	for i, got := range runnable.checkpointIDs {
		if got != want {
			t.Fatalf("checkpoint id[%d] = %q, want %q", i, got, want)
		}
	}
}

func assertGraphAgentIteratorClean(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) {
	t.Helper()
	seen := 0
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		seen++
		if event.Err != nil {
			t.Fatalf("unexpected agent error: %v", event.Err)
		}
	}
	if seen == 0 {
		t.Fatal("expected at least one agent event")
	}
}

func TestAgentGraphReplansAfterFailedStep(t *testing.T) {
	ctx := withGraphTestContext(context.Background())

	model := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"try failing search","status":"pending"},{"id":"s2","action":"recover","status":"pending","depends_on":["s1"]}]}`, nil),
			makeAssistantMessage(makeToolCall("call_1", "search", `{"query":"broken"}`)),
			schema.AssistantMessage(`{"decision":"replan","reason":"tool failed"}`, nil),
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"try recovery search","status":"pending"}]}`, nil),
			makeAssistantMessage(makeToolCall("call_2", "backup_search", `{"query":"recovery"}`)),
		},
	}

	tools := []einotool.BaseTool{
		&stubTool{name: "search", description: "search things", shouldErr: true},
		&stubTool{name: "backup_search", description: "backup search things", result: "recovered"},
	}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier("search", "backup_search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	ctx = safeParallelLifecycleContextFrom(t, ctx, tools)
	toolInfos := make([]*schema.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			t.Fatalf("tool Info: %v", err)
		}
		toolInfos = append(toolInfos, info)
	}

	runnable := buildTestAgentGraph(t, ctx, model, safeNode, 10, nil, toolInfos, nil)
	out, err := runnable.Invoke(ctx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("recover from failure")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got, want := out.Content, "recovered"; got != want {
		t.Fatalf("final content = %q, want %q", got, want)
	}
	if got, want := model.callCount, 5; got != want {
		t.Fatalf("model callCount = %d, want %d", got, want)
	}
}

func TestBuildAgentGraphWithCheckpointStore(t *testing.T) {
	ctx := withGraphTestContext(context.Background())
	model := &stubChatModel{responses: []string{`{"steps":[{"id":"s1","action":"hello","status":"pending"}]}`, "hello", `{"decision":"done"}`}}
	tools := []einotool.BaseTool{
		&stubTool{name: "search", description: "search things", result: "found"},
	}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}

	dir := t.TempDir()
	store, err := storesqlite.Open(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	runnable := buildTestAgentGraph(t, ctx, model, safeNode, 10, store, nil, nil)
	if runnable == nil {
		t.Fatal("buildAgentGraph returned nil runnable")
	}
}

type toolBindingRecorder struct {
	stubChatModel
	boundTools []*schema.ToolInfo
}

func (m *toolBindingRecorder) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	m.boundTools = tools
	return m, nil
}

func TestBuildAgentGraphDoesNotEagerBindToolsAtBuildTime(t *testing.T) {
	ctx := withGraphTestContext(context.Background())
	model := &toolBindingRecorder{stubChatModel: stubChatModel{responses: []string{`{"steps":[{"id":"s1","action":"hello","status":"pending"}]}`, "hello", `{"decision":"done"}`}}}
	tools := []einotool.BaseTool{
		&stubTool{name: "search", description: "search things", result: "found"},
		&stubTool{name: "compute", description: "compute things", result: "42"},
	}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), tools, fixedReadOnlyClassifier("search", "compute"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}

	var toolInfos []*schema.ToolInfo
	for _, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			t.Fatalf("tool Info: %v", err)
		}
		toolInfos = append(toolInfos, info)
	}

	buildTestAgentGraph(t, ctx, model, safeNode, 10, nil, toolInfos, nil)

	if len(model.boundTools) != 0 {
		t.Fatalf("expected graph build to avoid eager WithTools binding, got %d tools", len(model.boundTools))
	}
}

func TestErrorsAsInterruptSignal(t *testing.T) {
	signal := adk.FromInterruptContexts([]*adk.InterruptCtx{
		{ID: "test-id", Address: adk.Address{}, Info: map[string]any{"kind": "run_command_pause"}},
	})

	wrapped := fmt.Errorf("agent loop get tool results: %w", signal)

	var target *adk.InterruptSignal
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As failed to unwrap *adk.InterruptSignal through fmt.Errorf %w wrapping")
	}
	if target.ID != signal.ID {
		t.Fatalf("unwrapped signal ID = %q, want %q", target.ID, signal.ID)
	}

	doubleWrapped := fmt.Errorf("compose graph invoke: %w", wrapped)
	if _, ok := errors.AsType[*adk.InterruptSignal](doubleWrapped); !ok {
		t.Fatal("errors.As failed to unwrap through two layers of fmt.Errorf %w wrapping")
	}
}

type schemaMessageWrapper struct {
	Type  string          `json:"__type"`
	Value *schema.Message `json:"value"`
}

type fixedClassifier struct {
	rules map[string]tooling.ParallelPolicy
}

func (c *fixedClassifier) ExecutionPolicy(toolName string, args map[string]any) (tooling.ToolExecutionPolicy, error) {
	if s, ok := c.rules[toolName]; ok {
		policy := tooling.ToolExecutionPolicy{ParallelPolicy: s}
		if s != tooling.ParallelPolicyNeverParallel {
			policy.PathArg = "path"
		}
		return policy, nil
	}
	return tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicyNeverParallel}, nil
}

func fixedReadOnlyClassifier(names ...string) *fixedClassifier {
	rules := make(map[string]tooling.ParallelPolicy, len(names))
	for _, name := range names {
		rules[name] = tooling.ParallelPolicyReadOnly
	}
	return &fixedClassifier{rules: rules}
}

func safeParallelLifecycleContextFrom(t *testing.T, ctx context.Context, tools []einotool.BaseTool) context.Context {
	t.Helper()
	return tooltest.WithLoadedTools(t, ctx, tools, nil)
}

func makeAssistantMessage(calls ...schema.ToolCall) *schema.Message {
	return &schema.Message{
		Role:      schema.Assistant,
		ToolCalls: calls,
	}
}
