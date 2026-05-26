package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/stream"
)

func TestProjectStreamItemToEventKeepsToolInterruptShape(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_1",
		Kind:  stream.StreamKindToolCallInterrupted,
		Payload: &stream.ToolCallInterruptedPayload{ToolCall: &stream.StreamToolCall{
			Name:              "run_command",
			Error:             "need approval",
			InterruptContexts: 1,
		}},
	})
	if kind != "tool.call.interrupted" {
		t.Fatalf("kind = %q, want tool.call.interrupted", kind)
	}
	body := payload.(map[string]any)
	if body["tool_name"] != "run_command" || body["interrupt_contexts"] != float64(1) {
		t.Fatalf("unexpected payload: %#v", body)
	}
}
