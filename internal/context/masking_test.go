package context

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestApplyMaskingKeepsRecentToolResults(t *testing.T) {
	msgs := []adk.Message{
		schema.ToolMessage("old result", "call_1"),
		schema.ToolMessage("recent result", "call_2"),
	}
	// Annotate: call_1 at turn 0, call_2 at turn 2 (current).
	msgs[0] = AnnotateMessageTurn(msgs[0], 0)
	msgs[1] = AnnotateMessageTurn(msgs[1], 2)

	result := applyMasking(msgs, 2, 2)
	// call_1: turn 0, current 2, diff=2 <= maskAfterTurns(2) => keep
	if result[0].Content != "old result" {
		t.Fatalf("call_1 content = %q, want kept", result[0].Content)
	}
	// call_2: recent, keep
	if result[1].Content != "recent result" {
		t.Fatalf("call_2 content = %q, want kept", result[1].Content)
	}
}

func TestApplyMaskingMasksOldToolResults(t *testing.T) {
	msgs := []adk.Message{
		schema.ToolMessage("old result content here", "call_1"),
		schema.ToolMessage("recent result", "call_2"),
	}
	// call_1 at turn 0, call_2 at turn 5 (current).
	msgs[0] = AnnotateMessageTurn(msgs[0], 0)
	msgs[1] = AnnotateMessageTurn(msgs[1], 5)

	result := applyMasking(msgs, 5, 2)
	// call_1: turn 0, current 5, diff=5 > 2 => mask
	if result[0].Content == "old result content here" {
		t.Fatalf("call_1 should be masked but content is original")
	}
	if !strings.Contains(result[0].Content, "elided") {
		t.Fatalf("call_1 masked content should contain 'elided': %q", result[0].Content)
	}
	// call_2: recent, keep
	if result[1].Content != "recent result" {
		t.Fatalf("call_2 content = %q, want kept", result[1].Content)
	}
}

func TestApplyMaskingLeavesNonToolMessagesUnchanged(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("user request"),
		schema.AssistantMessage("assistant response", nil),
		schema.ToolMessage("tool result", "call_1"),
	}
	msgs[2] = AnnotateMessageTurn(msgs[2], 0)

	result := applyMasking(msgs, 10, 1)
	if result[0].Content != "user request" {
		t.Fatalf("user message changed: %q", result[0].Content)
	}
	if result[1].Content != "assistant response" {
		t.Fatalf("assistant message changed: %q", result[1].Content)
	}
	// tool message should be masked
	if result[2].Content == "tool result" {
		t.Fatalf("tool message should be masked")
	}
}

func TestApplyMaskingDisabledWhenMaskAfterTurnsZero(t *testing.T) {
	msgs := []adk.Message{
		schema.ToolMessage("tool result", "call_1"),
	}
	msgs[0] = AnnotateMessageTurn(msgs[0], 0)

	result := applyMasking(msgs, 100, 0)
	if result[0].Content != "tool result" {
		t.Fatalf("masking should be disabled when maskAfterTurns=0: %q", result[0].Content)
	}
}

func TestApplyMaskingHandlesEmptyMessages(t *testing.T) {
	result := applyMasking(nil, 5, 2)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d messages", len(result))
	}
}
