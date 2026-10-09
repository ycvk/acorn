package runtime

import (
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

func TestModelUsageIncludesStreamTailAndEverySuccessfulCall(t *testing.T) {
	p := &agentEventProjector{messagePrefix: "run"}
	usage := &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20}}
	items := collectProjected(t, p,
		adk.EventFromMessage(nil, schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "hello"}, {ResponseMeta: usage}}), schema.Assistant, ""),
		adk.EventFromMessage(&schema.Message{Role: schema.Assistant, Content: "world", ResponseMeta: usage}, nil, schema.Assistant, ""),
		adk.EventFromMessage(schema.AssistantMessage("unknown", nil), nil, schema.Assistant, ""),
	)
	var calls []core.StreamItem
	for _, item := range items {
		if string(item.Kind) == core.EventModelUsage {
			calls = append(calls, item)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("usage calls=%v", calls)
	}
	for _, call := range calls[:2] {
		if call.Payload["reported"] != true || call.Payload["prompt_tokens"] != 12 || call.Payload["completion_tokens"] != 8 || call.Payload["total_tokens"] != 20 {
			t.Fatalf("usage=%v", call.Payload)
		}
	}
	if calls[2].Payload["reported"] != false {
		t.Fatalf("missing usage=%v", calls[2])
	}
}

func TestModelUsagePersistenceFailureIsSurfaced(t *testing.T) {
	p := &agentEventProjector{messagePrefix: "run"}
	failure := errors.New("usage storage failure")
	err := p.project(adk.EventFromMessage(schema.AssistantMessage("ok", nil), nil, schema.Assistant, ""), func(item core.StreamItem) error {
		if string(item.Kind) == core.EventModelUsage {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}
