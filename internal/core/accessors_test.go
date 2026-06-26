package core

import (
	"context"
	"testing"
	"time"
)

// --- ItemGetMessage: typed-pointer and map paths ---

func TestItemGetMessageTypedPointer(t *testing.T) {
	msg := &StreamMessage{Role: "assistant", Content: "hello"}
	item := StreamItem{Kind: StreamKindAssistantMessage, Payload: map[string]any{"message": msg}}
	got := ItemGetMessage(item)
	if got == nil || got.Role != "assistant" || got.Content != "hello" {
		t.Fatalf("got %+v, want role=assistant content=hello", got)
	}
}

func TestItemGetMessageFromMap(t *testing.T) {
	item := StreamItem{Kind: StreamKindAssistantMessage, Payload: map[string]any{
		"message": map[string]any{
			"role":         "assistant",
			"content":      "hello",
			"reasoning":    "thinking",
			"tool_call_id": "call_1",
			"tool_name":    "file_read",
		},
	}}
	got := ItemGetMessage(item)
	if got == nil {
		t.Fatal("got nil")
	}
	if got.Role != "assistant" || got.Content != "hello" || got.Reasoning != "thinking" ||
		got.ToolCallID != "call_1" || got.ToolName != "file_read" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestItemGetMessageNilPayload(t *testing.T) {
	item := StreamItem{Kind: StreamKindAssistantMessage}
	if got := ItemGetMessage(item); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// --- ItemGetAssistantDelta: typed-pointer and map paths ---

func TestItemGetAssistantDeltaTypedPointer(t *testing.T) {
	delta := &StreamAssistantDelta{Role: "assistant", Delta: "chunk", Sequence: 3, MessageID: "msg_1", IsFinal: true}
	item := StreamItem{Kind: StreamKindAssistantDelta, Payload: map[string]any{"assistant_delta": delta}}
	got := ItemGetAssistantDelta(item)
	if got == nil || got.Delta != "chunk" || got.Sequence != 3 || !got.IsFinal {
		t.Fatalf("got %+v", got)
	}
}

func TestItemGetAssistantDeltaFromMap(t *testing.T) {
	item := StreamItem{Kind: StreamKindAssistantDelta, Payload: map[string]any{
		"assistant_delta": map[string]any{
			"role":       "assistant",
			"delta":      "chunk",
			"reasoning":  "thinking",
			"sequence":   42,
			"message_id": "msg_1",
			"is_final":   true,
			"meta":       map[string]any{"foo": "bar"},
		},
	}}
	got := ItemGetAssistantDelta(item)
	if got == nil {
		t.Fatal("got nil")
	}
	if got.Delta != "chunk" || got.Sequence != 42 || got.MessageID != "msg_1" || !got.IsFinal {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Meta == nil || got.Meta["foo"] != "bar" {
		t.Fatalf("meta: %+v", got.Meta)
	}
}

func TestItemGetAssistantDeltaMissing(t *testing.T) {
	item := StreamItem{Kind: StreamKindAssistantDelta, Payload: map[string]any{}}
	if got := ItemGetAssistantDelta(item); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// --- ItemGetInterrupt: typed-pointer, map, and nested contexts ---

func TestItemGetInterruptTypedPointer(t *testing.T) {
	intr := &StreamInterrupt{ContextCount: 2}
	item := StreamItem{Kind: StreamKindRunInterrupted, Payload: map[string]any{"interrupt": intr}}
	got := ItemGetInterrupt(item)
	if got == nil || got.ContextCount != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestItemGetInterruptFromMap(t *testing.T) {
	item := StreamItem{Kind: StreamKindRunInterrupted, Payload: map[string]any{
		"interrupt": map[string]any{
			"context_count": 1,
			"contexts": []any{
				map[string]any{
					"id":            "ctx_1",
					"address":       "0x100",
					"is_root_cause": true,
					"info": map[string]any{
						"kind":      "operator_question",
						"action_id": "act_1",
						"question":  "Allow?",
					},
				},
				map[string]any{
					"id":      "ctx_2",
					"address": "0x200",
					"info":    "not a map", // should be compacted to nil
				},
				"invalid", // should be skipped
			},
		},
	}}
	got := ItemGetInterrupt(item)
	if got == nil {
		t.Fatal("got nil")
	}
	if got.ContextCount != 1 {
		t.Fatalf("context_count: got %d, want 1", got.ContextCount)
	}
	if len(got.Contexts) != 2 {
		t.Fatalf("contexts: got %d, want 2", len(got.Contexts))
	}
	c1 := got.Contexts[0]
	if c1.ID != "ctx_1" || c1.Address != "0x100" || !c1.IsRootCause {
		t.Fatalf("ctx1: %+v", c1)
	}
	info, ok := c1.Info.(map[string]any)
	if !ok || info["action_id"] != "act_1" {
		t.Fatalf("ctx1 info: %+v", c1.Info)
	}
	// CompactInterruptInfo drops unknown keys
	if _, exists := info["question"]; !exists {
		t.Fatal("question key should be preserved by CompactInterruptInfo")
	}

	c2 := got.Contexts[1]
	if c2.ID != "ctx_2" {
		t.Fatalf("ctx2: %+v", c2)
	}
	if c2.Info != nil {
		t.Fatalf("ctx2 info should be nil for non-map, got %v", c2.Info)
	}
}

func TestItemGetInterruptMissing(t *testing.T) {
	item := StreamItem{Kind: StreamKindRunInterrupted, Payload: map[string]any{}}
	if got := ItemGetInterrupt(item); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// --- ItemGetError ---

func TestItemGetError(t *testing.T) {
	item := StreamItem{Kind: StreamKindRunFailed, Payload: map[string]any{"error": "boom"}}
	if got := ItemGetError(item); got != "boom" {
		t.Fatalf("got %q, want boom", got)
	}
}

func TestItemGetErrorMissing(t *testing.T) {
	item := StreamItem{Kind: StreamKindRunFailed, Payload: map[string]any{}}
	if got := ItemGetError(item); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// --- InterruptPayloadFromStream ---

func TestInterruptPayloadFromStreamNil(t *testing.T) {
	if got := InterruptPayloadFromStream(nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestInterruptPayloadFromStream(t *testing.T) {
	intr := &StreamInterrupt{
		ContextCount: 2,
		Contexts: []StreamInterruptContext{
			{ID: "ctx_1", Address: "0x100", Info: map[string]any{"kind": "command"}, IsRootCause: true},
			{ID: "ctx_2", Address: "0x200"},
		},
	}
	payload := InterruptPayloadFromStream(intr)
	if payload["context_count"] != 2 {
		t.Fatalf("context_count: got %v, want 2", payload["context_count"])
	}
	contexts, ok := payload["contexts"].([]map[string]any)
	if !ok {
		t.Fatalf("contexts type: %T", payload["contexts"])
	}
	if len(contexts) != 2 {
		t.Fatalf("len: got %d, want 2", len(contexts))
	}
	if contexts[0]["id"] != "ctx_1" || contexts[0]["is_root_cause"] != true {
		t.Fatalf("ctx1: %+v", contexts[0])
	}
}

// --- DurableContext ---

func TestDurableContextPreservesValues(t *testing.T) {
	ctx := WithRunID(context.Background(), "run_99")
	durable := DurableContext(ctx)
	if got := GetRunID(durable); got != "run_99" {
		t.Fatalf("got %q, want run_99", got)
	}
}

func TestDurableContextSurvivesCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	durable := DurableContext(ctx)
	select {
	case <-durable.Done():
		t.Fatal("durable context should not be cancelled")
	default:
	}
}

// --- Context plumbing roundtrip ---

func TestRunIDContextRoundtrip(t *testing.T) {
	ctx := context.Background()
	if got := GetRunID(ctx); got != "" {
		t.Fatalf("empty ctx: got %q", got)
	}
	ctx = WithRunID(ctx, "run_123")
	if got := GetRunID(ctx); got != "run_123" {
		t.Fatalf("got %q, want run_123", got)
	}
	if got := CurrentRunID(ctx); got != "run_123" {
		t.Fatalf("CurrentRunID: got %q", got)
	}
}

func TestSessionIDContextRoundtrip(t *testing.T) {
	ctx := WithSessionID(context.Background(), "sess_456")
	if got := GetSessionID(ctx); got != "sess_456" {
		t.Fatalf("got %q", got)
	}
}

func TestTurnIndexContextRoundtrip(t *testing.T) {
	ctx := WithTurnIndex(context.Background(), 7)
	if got := TurnIndexFromContext(ctx); got != 7 {
		t.Fatalf("got %d, want 7", got)
	}
	// default zero
	if got := TurnIndexFromContext(context.Background()); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// --- StreamItem JSON roundtrip ---

func TestStreamItemJSONRoundtrip(t *testing.T) {
	original := StreamItem{
		RunID:     "run_1",
		Sequence:  42,
		Kind:      StreamKindAssistantMessage,
		CreatedAt: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"message": "hello"},
	}
	data, err := original.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded StreamItem
	if err := decoded.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if decoded.RunID != "run_1" || decoded.Sequence != 42 || decoded.Kind != StreamKindAssistantMessage {
		t.Fatalf("decoded: %+v", decoded)
	}
	if decoded.Payload["message"] != "hello" {
		t.Fatalf("payload: %+v", decoded.Payload)
	}
}

func TestStreamItemJSONMissingRunID(t *testing.T) {
	data := []byte(`{"kind":"assistant_message"}`)
	var item StreamItem
	if err := item.UnmarshalJSON(data); err == nil {
		t.Fatal("expected error for missing run_id")
	}
}

func TestStreamItemJSONMissingKind(t *testing.T) {
	data := []byte(`{"run_id":"run_1"}`)
	var item StreamItem
	if err := item.UnmarshalJSON(data); err == nil {
		t.Fatal("expected error for missing kind")
	}
}
