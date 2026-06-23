package stream

import (
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func TestProjectStreamItemToEventNormalizesToolInterruptPayload(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, domain.StreamItem{
		RunID: "run_1",
		Kind:  domain.StreamKindToolCallInterrupted,
		Payload: map[string]any{"tool_call": &StreamToolCall{
			Name:              "run_command",
			Error:             "need approval",
			InterruptContexts: 1,
		}},
	})
	if kind != "tool.call.interrupted" {
		t.Fatalf("kind = %q, want tool.call.interrupted", kind)
	}
	body := payload.(map[string]any)
	if _, ok := body["tool_call"]; ok {
		t.Fatalf("tool_call should be normalized out of event payload: %#v", body)
	}
	if body["tool_name"] != "run_command" || body["interrupt_contexts"] != float64(1) {
		t.Fatalf("unexpected normalized tool payload: %#v", body)
	}
}
