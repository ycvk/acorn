package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// mockEventAppender implements core.EventAppender for testing.
type mockEventAppender struct {
	record core.EventRecord
	err    error
}

func (m *mockEventAppender) AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	return m.record, m.err
}

func TestAppendStreamItemNilStore(t *testing.T) {
	item := core.StreamItem{RunID: "run_1", Kind: core.StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), nil, nil, item)
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestAppendStreamItemSuccess(t *testing.T) {
	now := time.Now()
	store := &mockEventAppender{
		record: core.EventRecord{
			Sequence:  42,
			RunID:     "run_1",
			Kind:      "run.started",
			CreatedAt: now,
		},
	}

	var sinkCalled bool
	sink := func(item core.StreamItem) error {
		sinkCalled = true
		if item.Sequence != 42 {
			t.Fatalf("sink: sequence = %d, want 42", item.Sequence)
		}
		return nil
	}

	item := core.StreamItem{RunID: "run_1", Kind: core.StreamKindRunStarted, Payload: map[string]any{"input": "hello"}}
	record, err := AppendStreamItem(context.Background(), store, sink, item)
	if err != nil {
		t.Fatalf("AppendStreamItem: %v", err)
	}
	if record.Sequence != 42 {
		t.Fatalf("sequence = %d", record.Sequence)
	}
	if !sinkCalled {
		t.Fatal("sink was not called")
	}
}

func TestAppendStreamItemStoreError(t *testing.T) {
	store := &mockEventAppender{err: errors.New("db error")}
	item := core.StreamItem{RunID: "run_1", Kind: core.StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), store, nil, item)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAppendStreamItemSinkError(t *testing.T) {
	store := &mockEventAppender{record: core.EventRecord{Sequence: 1}}
	sink := func(item core.StreamItem) error { return errors.New("sink error") }
	item := core.StreamItem{RunID: "run_1", Kind: core.StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), store, sink, item)
	if err == nil {
		t.Fatal("expected sink error")
	}
}

func TestAppendStreamItemNoSink(t *testing.T) {
	store := &mockEventAppender{record: core.EventRecord{Sequence: 1}}
	item := core.StreamItem{RunID: "run_1", Kind: core.StreamKindRunCompleted}
	record, err := AppendStreamItem(context.Background(), store, nil, item)
	if err != nil {
		t.Fatalf("AppendStreamItem: %v", err)
	}
	if record.Sequence != 1 {
		t.Fatalf("sequence = %d", record.Sequence)
	}
}

func TestProjectStreamItemToEvent(t *testing.T) {
	cases := []struct {
		name     string
		item     core.StreamItem
		wantKind string
	}{
		{
			name:     "nil_payload",
			item:     core.StreamItem{Kind: core.StreamKindRunCompleted},
			wantKind: "run.completed",
		},
		{
			name:     "run_started",
			item:     core.StreamItem{Kind: core.StreamKindRunStarted, Payload: map[string]any{"input": "hello"}},
			wantKind: "run.started",
		},
		{
			name:     "tool_call_started",
			item:     core.StreamItem{Kind: core.StreamKindToolCallStarted, Payload: map[string]any{"tool_call": &core.StreamToolCall{Name: "t1", CallID: "c1"}}},
			wantKind: "tool.call.started",
		},
		{
			name:     "assistant_message",
			item:     core.StreamItem{Kind: core.StreamKindAssistantMessage, Payload: map[string]any{"message": &core.StreamMessage{Content: "hi"}}},
			wantKind: "runtime.message",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, payload, err := ProjectStreamItemToEvent(tc.item)
			if err != nil {
				t.Fatalf("ProjectStreamItemToEvent: %v", err)
			}
			if kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", kind, tc.wantKind)
			}
			if payload == nil {
				t.Fatal("payload is nil")
			}
		})
	}
}

func TestStreamKindToEventKind(t *testing.T) {
	cases := []struct {
		kind core.StreamItemKind
		want string
	}{
		{core.StreamKindRunStarted, "run.started"},
		{core.StreamKindRunCompleted, "run.completed"},
		{core.StreamKindRunFailed, "run.failed"},
		{core.StreamKindRunInterrupted, "run.interrupted"},
		{core.StreamKindRunResumeRequested, "run.resume_requested"},
		{core.StreamKindAssistantDelta, "assistant.delta"},
		{core.StreamKindAssistantMessage, "runtime.message"},
		{core.StreamKindToolCallStarted, "tool.call.started"},
		{core.StreamKindToolCallSucceeded, "tool.call.succeeded"},
		{core.StreamKindToolCallFailed, "tool.call.failed"},
		{core.StreamKindToolCallInterrupted, "tool.call.interrupted"},
		{core.StreamKindSkillDiscovered, "skill.discovered"},
		{core.StreamKindSkillSelected, "skill.selected"},
		{core.StreamKindSkillLoaded, "skill.loaded"},
		{core.StreamKindSkillFailed, "skill.failed"},
		{core.StreamKindProcedureActivation, "procedure.activation"},
		{core.StreamKindMemoryPrepared, "memory.prepared"},
		{"unknown.kind", "unknown.kind"},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := streamKindToEventKind(tc.kind); got != tc.want {
				t.Fatalf("streamKindToEventKind(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}
