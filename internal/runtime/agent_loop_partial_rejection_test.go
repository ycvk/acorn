package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/tools/dispatch"
)

// multiCallTestExecutor is a StreamingExecutor that records submitted calls
// and returns a tool result for each, keyed by call ID. Unlike
// directResponseTestStreamingExecutor it handles multiple calls correctly.
type multiCallTestExecutor struct {
	submitted []schema.ToolCall
	results   map[string]string // callID -> result text
}

func (e *multiCallTestExecutor) Submit(call schema.ToolCall) {
	e.submitted = append(e.submitted, call)
}

func (e *multiCallTestExecutor) GetRemainingResults(_ context.Context) ([]*schema.Message, error) {
	var msgs []*schema.Message
	for _, call := range e.submitted {
		text, ok := e.results[call.ID]
		if !ok {
			text = "default result"
		}
		msgs = append(msgs, schema.ToolMessage(text, call.ID, schema.WithToolName(call.Function.Name)))
	}
	return msgs, nil
}

func (e *multiCallTestExecutor) Discard() {}

// multiCallTestToolNode returns a multiCallTestExecutor from NewStreamingExecutor.
type multiCallTestToolNode struct {
	executors []*multiCallTestExecutor
}

func (n *multiCallTestToolNode) NewStreamingExecutor(_ context.Context) dispatch.StreamingExecutor {
	e := &multiCallTestExecutor{results: make(map[string]string)}
	n.executors = append(n.executors, e)
	return e
}

func TestExecuteRoundPartialRejectionPreservesAcceptedToolResults(t *testing.T) {
	// BUG 11: when BeforeToolCall rejects one tool call in a batch, the
	// already-submitted tool calls should still return their results.
	// Before the fix, executor.Discard() threw away ALL submitted calls.
	safeCall := schema.ToolCall{
		ID: "call_safe",
		Function: schema.FunctionCall{
			Name:      "file_read",
			Arguments: `{"path":"/etc/hosts"}`,
		},
	}
	riskyCall := schema.ToolCall{
		ID: "call_risky",
		Function: schema.FunctionCall{
			Name:      "file_delete",
			Arguments: `{"path":"/tmp/important"}`,
		},
	}
	model := &directResponseTestModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{safeCall, riskyCall}),
	}}
	streamer := &directResponseTestStreamer{}
	toolNode := &multiCallTestToolNode{}

	_, toolMessages, _, err := ExecuteRound(
		context.Background(),
		model,
		streamer,
		toolNode,
		[]*schema.Message{schema.UserMessage("read then delete")},
		nil,
		"run_partial",
		"msg_partial",
		RoundOptions{
			BeforeToolCall: func(_ context.Context, call schema.ToolCall) error {
				if call.Function.Name == "file_delete" {
					return &ApprovalRequiredError{ToolName: call.Function.Name, CallID: call.ID}
				}
				return nil
			},
		},
	)

	// The rejection should surface as an error.
	var are *ApprovalRequiredError
	if !errors.As(err, &are) {
		t.Fatalf("error = %T %[1]v, want ApprovalRequiredError", err)
	}

	// The safe call's result must be preserved.
	if len(toolMessages) == 0 {
		t.Fatal("toolMessages is empty; want at least the accepted call result")
	}
	foundSafe := false
	for _, msg := range toolMessages {
		if msg.ToolCallID == "call_safe" {
			foundSafe = true
		}
	}
	if !foundSafe {
		t.Fatalf("toolMessages missing call_safe result; got %d messages", len(toolMessages))
	}

	// The risky call must NOT have been submitted.
	if len(toolNode.executors) == 0 {
		t.Fatal("no executor created")
	}
	for _, submitted := range toolNode.executors[0].submitted {
		if submitted.ID == "call_risky" {
			t.Fatal("risky call was submitted to executor; should have been skipped")
		}
	}
}
