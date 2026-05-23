package runprojection

import "testing"

func TestProjectStreamItemToEventKeepsToolInterruptShape(t *testing.T) {
	kind, payload := MustProjectStreamItemToEvent(t, StreamItem{
		RunID: "run_1",
		Kind:  StreamKindToolCallInterrupted,
		Payload: &ToolCallInterruptedPayload{ToolCall: &StreamToolCall{
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
