package runtime

import (
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

func collectProjected(t *testing.T, p *agentEventProjector, events ...*adk.AgentEvent) []core.StreamItem {
	t.Helper()
	var items []core.StreamItem
	for _, event := range events {
		if err := p.project(event, func(item core.StreamItem) error {
			items = append(items, item)
			return nil
		}); err != nil {
			t.Fatalf("project: %v", err)
		}
	}
	return items
}

func TestProjectStreamingAssistantEmitsDeltasThenMessage(t *testing.T) {
	stream := schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "Hel"},
		{Role: schema.Assistant, Content: ""},
		{Role: schema.Assistant, Content: "lo"},
	})
	p := &agentEventProjector{messagePrefix: "run_x:assistant:7"}
	items := collectProjected(t, p,
		adk.EventFromMessage(nil, stream, schema.Assistant, ""),
		adk.EventFromMessage(nil, schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "again"}}), schema.Assistant, ""),
	)
	kinds := make([]core.StreamItemKind, 0, len(items))
	for _, item := range items {
		kinds = append(kinds, item.Kind)
	}
	want := []core.StreamItemKind{
		core.StreamKindAssistantDelta, core.StreamKindAssistantDelta, core.StreamKindAssistantMessage,
		core.StreamKindAssistantDelta, core.StreamKindAssistantMessage,
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	first := core.ItemGetAssistantDelta(items[0])
	second := core.ItemGetAssistantDelta(items[1])
	if first.MessageID != "run_x:assistant:7:1" || first.Sequence != 1 || second.Sequence != 2 || second.Delta != "lo" {
		t.Fatalf("deltas = %+v / %+v", first, second)
	}
	if msg := core.ItemGetMessage(items[2]); msg == nil || msg.Content != "Hello" {
		t.Fatalf("assembled message = %+v", msg)
	}
	if next := core.ItemGetAssistantDelta(items[3]); next.MessageID != "run_x:assistant:7:2" || next.Sequence != 1 {
		t.Fatalf("second assistant delta = %+v", next)
	}
}

func TestProjectToolResultInterruptAndError(t *testing.T) {
	toolMsg := schema.ToolMessage("result", "call_1", schema.WithToolName("search_runs"))
	interrupted := &adk.AgentEvent{Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{}}}
	failed := &adk.AgentEvent{Err: errors.New("boom")}
	items := collectProjected(t, &agentEventProjector{messagePrefix: "run_x:assistant:7"},
		adk.EventFromMessage(toolMsg, nil, schema.Tool, "search_runs"),
		interrupted,
		failed,
	)
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Kind != core.StreamKindToolCallSucceeded {
		t.Fatalf("first kind = %s", items[0].Kind)
	}
	payload := items[0].Payload
	if payload["tool_call_id"] != "call_1" || payload["tool_name"] != "search_runs" || payload["output"] != "result" {
		t.Fatalf("tool payload = %v", payload)
	}
	if items[1].Kind != core.StreamKindRunInterrupted || items[2].Kind != core.StreamKindRunFailed {
		t.Fatalf("kinds = %s, %s", items[1].Kind, items[2].Kind)
	}
	if core.ItemGetError(items[2]) != "boom" {
		t.Fatalf("error = %q", core.ItemGetError(items[2]))
	}
}
