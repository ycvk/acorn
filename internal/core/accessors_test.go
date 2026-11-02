package core

import (
	"context"
	"testing"
)

// --- ItemGetMessage ---

func TestItemGetMessageTypedPointer(t *testing.T) {
	msg := &StreamMessage{Role: "assistant", Content: "hello"}
	item := StreamItem{Kind: StreamKindAssistantMessage, Payload: map[string]any{"message": msg}}
	got := ItemGetMessage(item)
	if got == nil || got.Role != "assistant" || got.Content != "hello" {
		t.Fatalf("got %+v, want role=assistant content=hello", got)
	}
}

func TestItemGetMessageNilPayload(t *testing.T) {
	item := StreamItem{Kind: StreamKindAssistantMessage}
	if got := ItemGetMessage(item); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// --- ItemGetAssistantDelta ---

func TestItemGetAssistantDeltaTypedPointer(t *testing.T) {
	delta := &StreamAssistantDelta{Role: "assistant", Delta: "chunk", Sequence: 3, MessageID: "msg_1", IsFinal: true}
	item := StreamItem{Kind: StreamKindAssistantDelta, Payload: map[string]any{"assistant_delta": delta}}
	got := ItemGetAssistantDelta(item)
	if got == nil || got.Delta != "chunk" || got.Sequence != 3 || !got.IsFinal {
		t.Fatalf("got %+v", got)
	}
}

func TestItemGetAssistantDeltaMissing(t *testing.T) {
	item := StreamItem{Kind: StreamKindAssistantDelta, Payload: map[string]any{}}
	if got := ItemGetAssistantDelta(item); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// --- ItemGetInterrupt ---

func TestItemGetInterruptTypedPointer(t *testing.T) {
	intr := &StreamInterrupt{ContextCount: 2}
	item := StreamItem{Kind: StreamKindRunInterrupted, Payload: map[string]any{"interrupt": intr}}
	got := ItemGetInterrupt(item)
	if got == nil || got.ContextCount != 2 {
		t.Fatalf("got %+v", got)
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

// --- Context plumbing roundtrip ---

func TestRunIDContextRoundtrip(t *testing.T) {
	ctx := context.Background()
	if got := CurrentRunID(ctx); got != "" {
		t.Fatalf("empty ctx: got %q", got)
	}
	ctx = WithRunID(ctx, "run_123")
	if got := CurrentRunID(ctx); got != "run_123" {
		t.Fatalf("got %q, want run_123", got)
	}
	if got := CurrentRunID(ctx); got != "run_123" {
		t.Fatalf("CurrentRunID: got %q", got)
	}
}

func TestSessionIDContextRoundtrip(t *testing.T) {
	ctx := WithSessionID(context.Background(), "sess_456")
	if got := CurrentSessionID(ctx); got != "sess_456" {
		t.Fatalf("got %q", got)
	}
}
