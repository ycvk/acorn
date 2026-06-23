package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestExecuteRoundRunsBeforeToolCallHookBeforeSubmit(t *testing.T) {
	toolCall := schema.ToolCall{
		ID: "call_blocked",
		Function: schema.FunctionCall{
			Name:      "lookup",
			Arguments: `{"query":"acorn"}`,
		},
	}
	model := &directResponseTestModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{toolCall}),
	}}
	streamer := &directResponseTestStreamer{}
	tools := &directResponseTestToolNode{}
	rejectErr := errors.New("blocked by policy")

	_, _, _, err := ExecuteRound(
		context.Background(),
		model,
		streamer,
		tools,
		[]*schema.Message{schema.UserMessage("search")},
		nil,
		"run_round_hook",
		"msg_round_hook",
		RoundOptions{
			BeforeToolCall: func(context.Context, schema.ToolCall) error {
				return rejectErr
			},
		},
	)
	if err == nil {
		t.Fatalf("ExecuteRound succeeded, want hook rejection")
	}
	var rejected *ToolCallRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("error = %T %[1]v, want ToolCallRejectedError", err)
	}
	if !errors.Is(err, rejectErr) {
		t.Fatalf("error = %v, want wrapped rejectErr", err)
	}
	if tools.calls != 0 {
		t.Fatalf("tool calls = %d, want 0 because hook must run before Submit", tools.calls)
	}
}
