package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

type failingTool struct{}

func (failingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "flaky_tool", Desc: "always fails"}, nil
}

func (failingTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "partial output", errors.New("upstream timed out")
}

// newToolErrorTestRunner mirrors production handler order: the given handlers
// (e.g. approval) wrap the tool error handler.
func newToolErrorTestRunner(t *testing.T, model *scriptedModel, failed *failedToolCalls, handlers ...adk.ChatModelAgentMiddleware) *adk.Runner {
	t.Helper()
	ctx := context.Background()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "tool_error_test",
		Description: "tool error test agent",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               []einotool.BaseTool{failingTool{}, &echoTool{}},
			ExecuteSequentially: true,
			UnknownToolsHandler: unknownToolResult,
		}},
		Handlers:      append(handlers, newToolErrorMiddleware(failed)),
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true, CheckPointStore: &memoryCheckpointStore{data: map[string][]byte{}}})
}

func TestToolFailureBecomesModelVisibleResult(t *testing.T) {
	model := &scriptedModel{replies: []*schema.Message{
		toolCallReply("call_1", "flaky_tool", `{}`),
		schema.AssistantMessage("recovered", nil),
	}}
	_, err := drainEvents(t, newToolErrorTestRunner(t, model, newFailedToolCalls()).Run(context.Background(), []adk.Message{schema.UserMessage("go")}))
	if err != nil {
		t.Fatalf("run failed instead of surfacing the tool error to the model: %v", err)
	}
	assertLastToolMessage(t, model, "Tool flaky_tool failed: upstream timed out")
}

func TestUnknownToolBecomesModelVisibleResult(t *testing.T) {
	model := &scriptedModel{replies: []*schema.Message{
		toolCallReply("call_1", "made_up_tool", `{}`),
		schema.AssistantMessage("ok", nil),
	}}
	_, err := drainEvents(t, newToolErrorTestRunner(t, model, newFailedToolCalls()).Run(context.Background(), []adk.Message{schema.UserMessage("go")}))
	if err != nil {
		t.Fatalf("unknown tool failed the run: %v", err)
	}
	assertLastToolMessage(t, model, "Tool made_up_tool does not exist")
}

func TestApprovalInterruptPassesThroughToolErrorMiddleware(t *testing.T) {
	RegisterTypes()
	approval, err := newApprovalMiddleware([]string{"echo_tool"}, &approvalTestStore{})
	if err != nil {
		t.Fatalf("approval middleware: %v", err)
	}
	model := &scriptedModel{replies: []*schema.Message{toolCallReply("call_1", "echo_tool", `{}`)}}
	ctx := core.WithRunID(context.Background(), "run_errors")
	interrupted, err := drainEvents(t, newToolErrorTestRunner(t, model, newFailedToolCalls(), approval).Run(ctx, []adk.Message{schema.UserMessage("go")}, adk.WithCheckPointID("run_errors")))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if interrupted == nil {
		t.Fatal("the approval interrupt was swallowed by the tool error middleware")
	}
	for _, msg := range model.lastInput() {
		if msg.Role == schema.Tool && strings.Contains(msg.Content, "failed") {
			t.Fatalf("interrupt was turned into a tool failure: %q", msg.Content)
		}
	}
}

type failingApprovalStore struct{ approvalTestStore }

func (*failingApprovalStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, errors.New("database is locked")
}

func TestApprovalStoreFailureFailsRun(t *testing.T) {
	approval, err := newApprovalMiddleware([]string{"echo_tool"}, &failingApprovalStore{})
	if err != nil {
		t.Fatalf("approval middleware: %v", err)
	}
	model := &scriptedModel{replies: []*schema.Message{
		toolCallReply("call_1", "echo_tool", `{}`),
		schema.AssistantMessage("should not be reached", nil),
	}}
	ctx := core.WithRunID(context.Background(), "run_store_fail")
	_, err = drainEvents(t, newToolErrorTestRunner(t, model, newFailedToolCalls(), approval).Run(ctx, []adk.Message{schema.UserMessage("go")}))
	if err == nil || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("run error = %v, want the approval store failure to fail the run", err)
	}
}

func TestFailedToolCallProjectsAsFailed(t *testing.T) {
	model := &scriptedModel{replies: []*schema.Message{
		toolCallReply("call_1", "flaky_tool", `{}`),
		toolCallReply("call_2", "echo_tool", `{}`),
		schema.AssistantMessage("done", nil),
	}}
	failed := newFailedToolCalls()
	iter := newToolErrorTestRunner(t, model, failed).Run(context.Background(), []adk.Message{schema.UserMessage("go")})
	store := &eventLogStore{}
	state, err := (&Executor{store: store}).collectRunState(context.Background(), "run_tools", iter, nil, &ActiveRunner{ChatModel: model, FailedCalls: failed})
	if err != nil || state.failure != nil {
		t.Fatalf("collect: err=%v failure=%v", err, state.failure)
	}
	var toolEvents []string
	for _, event := range store.events {
		if strings.HasPrefix(event.kind, "tool.call.") {
			toolEvents = append(toolEvents, event.kind+":"+event.payload["tool_call_id"].(string))
		}
	}
	if strings.Join(toolEvents, ",") != "tool.call.failed:call_1,tool.call.succeeded:call_2" {
		t.Fatalf("tool events = %v", toolEvents)
	}
	if got := store.events[indexOfKind(store.events, "tool.call.failed")].payload["error"]; got != "upstream timed out" {
		t.Fatalf("failed payload error = %v", got)
	}
}
