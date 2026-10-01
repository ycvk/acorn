package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

type approvalTestStore struct {
	mu      sync.Mutex
	actions []core.PendingActionInput
	events  []string
}

func (s *approvalTestStore) CreatePendingAction(_ context.Context, input core.PendingActionInput) (*core.PendingActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = append(s.actions, input)
	return &core.PendingActionRecord{ActionID: input.ActionID, RunID: input.RunID, Kind: input.Kind, Status: input.Status}, nil
}

func (s *approvalTestStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, kind)
	return core.EventRecord{RunID: runID, Kind: kind, Payload: payload}, nil
}

type memoryCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *memoryCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[id]
	return data, ok, nil
}

func (s *memoryCheckpointStore) Set(_ context.Context, id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = append([]byte(nil), data...)
	return nil
}

type echoTool struct {
	mu    sync.Mutex
	calls []string
}

func (t *echoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "echo_tool", Desc: "echoes its arguments"}, nil
}

func (t *echoTool) InvokableRun(_ context.Context, args string, _ ...einotool.Option) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, args)
	return "echo:" + args, nil
}

func (t *echoTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func newApprovalTestRunner(t *testing.T, model *scriptedModel, tool *echoTool, store *approvalTestStore, checkpoints adk.CheckPointStore) *adk.Runner {
	t.Helper()
	ctx := context.Background()
	mw, err := newApprovalMiddleware([]string{"echo_*"}, store)
	if err != nil {
		t.Fatalf("approval middleware: %v", err)
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "approval_test",
		Description: "approval test agent",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               []einotool.BaseTool{tool},
			ExecuteSequentially: true,
		}},
		Handlers:      []adk.ChatModelAgentMiddleware{mw},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("new chat model agent: %v", err)
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true, CheckPointStore: checkpoints})
}

// drainEvents consumes every event, closing message streams, and returns the
// interrupt (if any) plus the first error event.
func drainEvents(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) (*adk.InterruptInfo, error) {
	t.Helper()
	var interrupted *adk.InterruptInfo
	for {
		event, ok := iter.Next()
		if !ok {
			return interrupted, nil
		}
		if event.Err != nil {
			return interrupted, event.Err
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			if _, err := event.Output.MessageOutput.GetMessage(); err != nil {
				t.Fatalf("read event message: %v", err)
			}
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = event.Action.Interrupted
		}
	}
}

// interruptAcrossRunners runs the agent until the approval interrupt with one
// runner and returns the interrupt id so a second, fresh runner can resume.
func interruptAcrossRunners(t *testing.T, ctx context.Context, store *approvalTestStore, checkpoints adk.CheckPointStore, tool *echoTool) string {
	t.Helper()
	first := &scriptedModel{replies: []*schema.Message{toolCallReply("call_1", "echo_tool", `{"v":1}`)}}
	runner := newApprovalTestRunner(t, first, tool, store, checkpoints)
	interrupted, err := drainEvents(t, runner.Run(ctx, []adk.Message{schema.UserMessage("go")}, adk.WithCheckPointID("run_approval")))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if interrupted == nil || len(interrupted.InterruptContexts) == 0 {
		t.Fatal("expected the approval interrupt")
	}
	var rootID string
	for _, ic := range interrupted.InterruptContexts {
		if !ic.IsRootCause {
			continue
		}
		info, ok := ic.Info.(map[string]any)
		if !ok || info["kind"] != toolApprovalInterruptKind || info["action_id"] != "tool_approval:run_approval:call_1" {
			t.Fatalf("unexpected interrupt info: %#v", ic.Info)
		}
		rootID = ic.ID
	}
	if rootID == "" {
		t.Fatal("no root-cause interrupt context")
	}
	if tool.callCount() != 0 {
		t.Fatalf("tool ran %d times before approval", tool.callCount())
	}
	if len(store.actions) != 1 || store.actions[0].Kind != core.PendingActionKindToolApproval || store.actions[0].RunID != "run_approval" {
		t.Fatalf("pending actions = %+v", store.actions)
	}
	if len(store.events) != 1 || store.events[0] != "tool_approval.pending" {
		t.Fatalf("events = %v", store.events)
	}
	return rootID
}

func TestApprovalAcceptRunsRecordedCallInFreshRunner(t *testing.T) {
	RegisterTypes()
	ctx := core.WithRunID(context.Background(), "run_approval")
	store := &approvalTestStore{}
	checkpoints := &memoryCheckpointStore{data: map[string][]byte{}}
	tool := &echoTool{}
	interruptID := interruptAcrossRunners(t, ctx, store, checkpoints, tool)

	second := &scriptedModel{replies: []*schema.Message{schema.AssistantMessage("done", nil)}}
	runner := newApprovalTestRunner(t, second, tool, store, checkpoints)
	iter, err := runner.ResumeWithParams(ctx, "run_approval", &adk.ResumeParams{Targets: map[string]any{
		interruptID: map[string]any{"action": "accept", "action_id": "tool_approval:run_approval:call_1"},
	}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if interrupted, err := drainEvents(t, iter); err != nil || interrupted != nil {
		t.Fatalf("resumed run: interrupted=%v err=%v", interrupted, err)
	}
	if tool.callCount() != 1 || tool.calls[0] != `{"v":1}` {
		t.Fatalf("tool calls = %v, want exactly the approved arguments", tool.calls)
	}
	assertLastToolMessage(t, second, "echo:{\"v\":1}")
}

func TestApprovalDeclineSkipsCall(t *testing.T) {
	RegisterTypes()
	ctx := core.WithRunID(context.Background(), "run_approval")
	store := &approvalTestStore{}
	checkpoints := &memoryCheckpointStore{data: map[string][]byte{}}
	tool := &echoTool{}
	interruptID := interruptAcrossRunners(t, ctx, store, checkpoints, tool)

	second := &scriptedModel{replies: []*schema.Message{schema.AssistantMessage("ok, skipped", nil)}}
	runner := newApprovalTestRunner(t, second, tool, store, checkpoints)
	iter, err := runner.ResumeWithParams(ctx, "run_approval", &adk.ResumeParams{Targets: map[string]any{
		interruptID: map[string]any{"action": "decline"},
	}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := drainEvents(t, iter); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if tool.callCount() != 0 {
		t.Fatalf("declined tool ran %d times", tool.callCount())
	}
	assertLastToolMessage(t, second, "The owner declined this echo_tool call")
}

func TestApprovalSkipsUnmatchedTools(t *testing.T) {
	mw, err := newApprovalMiddleware([]string{"mcp__*"}, &approvalTestStore{})
	if err != nil {
		t.Fatalf("approval middleware: %v", err)
	}
	if mw.requiresApproval("echo_tool") || !mw.requiresApproval("mcp__gmail__send") {
		t.Fatal("pattern matching is wrong")
	}
	if _, err := newApprovalMiddleware([]string{"["}, &approvalTestStore{}); err == nil {
		t.Fatal("expected malformed pattern to be rejected")
	}
}

func assertLastToolMessage(t *testing.T, model *scriptedModel, want string) {
	t.Helper()
	for _, msg := range model.lastInput() {
		if msg.Role == schema.Tool && msg.ToolCallID == "call_1" {
			if !strings.Contains(msg.Content, want) {
				t.Fatalf("tool message = %q, want it to contain %q", msg.Content, want)
			}
			return
		}
	}
	t.Fatalf("model input has no tool message for call_1: %+v", model.lastInput())
}
