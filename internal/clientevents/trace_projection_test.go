package clientevents

import (
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

func TestBuildTraceSummaryUsesClientEventProjectionTypes(t *testing.T) {
	raw := []events.EventRecord{
		{
			Sequence:  1,
			RunID:     "run_1",
			Kind:      "assistant.delta",
			CreatedAt: time.Now(),
			Payload: map[string]any{
				"assistant_delta": map[string]any{
					"delta":      "你",
					"message_id": "msg_1",
				},
			},
		},
		{
			Sequence:  2,
			RunID:     "run_1",
			Kind:      "assistant.delta",
			CreatedAt: time.Now(),
			Payload: map[string]any{
				"assistant_delta": map[string]any{
					"delta":      "好",
					"message_id": "msg_1",
				},
			},
		},
		{
			Sequence:  3,
			RunID:     "run_1",
			Kind:      "agent.message",
			CreatedAt: time.Now(),
			Payload: map[string]any{
				"message": map[string]any{"role": "assistant", "content": "你好"},
			},
		},
		{
			Sequence:  4,
			RunID:     "run_1",
			Kind:      "tool.call.succeeded",
			CreatedAt: time.Now(),
			Payload: map[string]any{
				"tool_name": "read_file",
				"output":    "ok",
			},
		},
		{
			Sequence:  5,
			RunID:     "run_1",
			Kind:      "skill.selected",
			CreatedAt: time.Now(),
			Payload: map[string]any{
				"skill": map[string]any{"selected_id": "skill.inspect.repo"},
			},
		},
		{
			Sequence:  6,
			RunID:     "run_1",
			Kind:      "plan.created",
			CreatedAt: time.Now(),
			Payload:   map[string]any{"plan_id": "plan_1"},
		},
		{
			Sequence:  7,
			RunID:     "run_1",
			Kind:      "run.completed",
			CreatedAt: time.Now(),
			Payload:   map[string]any{"message": map[string]any{"content": "done"}},
		},
	}

	summary, err := BuildTraceSummary(raw)
	if err != nil {
		t.Fatalf("BuildTraceSummary: %v", err)
	}
	if summary.ItemCount != 7 || summary.LastKind != "run_completed" {
		t.Fatalf("summary item identity = %#v", summary)
	}
	if summary.AssistantDeltaCount != 2 || summary.AssistantDeltaMessageCount != 1 || summary.AssistantDeltaCharCount != 2 {
		t.Fatalf("assistant delta counts = %#v", summary)
	}
	if summary.AssistantMessageCount != 1 || summary.ToolCallCount != 1 {
		t.Fatalf("message/tool counts = %#v", summary)
	}
	if summary.SkillEventCount != 1 || !summary.SkillSelected || summary.PlanEventCount != 1 || !summary.Completed {
		t.Fatalf("state counts = %#v", summary)
	}
}
