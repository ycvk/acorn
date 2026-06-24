package stream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/domain"
)

// mockEventAppender implements api.EventAppender for testing.
type mockEventAppender struct {
	record domain.EventRecord
	err    error
}

func (m *mockEventAppender) AppendEvent(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error) {
	return m.record, m.err
}

func TestAppendStreamItemNilStore(t *testing.T) {
	item := domain.StreamItem{RunID: "run_1", Kind: domain.StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), nil, nil, item)
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestAppendStreamItemSuccess(t *testing.T) {
	now := time.Now()
	store := &mockEventAppender{
		record: domain.EventRecord{
			Sequence:  42,
			RunID:     "run_1",
			Kind:      "run.started",
			CreatedAt: now,
		},
	}

	var sinkCalled bool
	sink := func(item domain.StreamItem) error {
		sinkCalled = true
		if item.Sequence != 42 {
			t.Fatalf("sink: sequence = %d, want 42", item.Sequence)
		}
		return nil
	}

	item := domain.StreamItem{RunID: "run_1", Kind: domain.StreamKindRunStarted, Payload: map[string]any{"input": "hello"}}
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
	item := domain.StreamItem{RunID: "run_1", Kind: domain.StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), store, nil, item)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAppendStreamItemSinkError(t *testing.T) {
	store := &mockEventAppender{record: domain.EventRecord{Sequence: 1}}
	sink := func(item domain.StreamItem) error { return errors.New("sink error") }
	item := domain.StreamItem{RunID: "run_1", Kind: domain.StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), store, sink, item)
	if err == nil {
		t.Fatal("expected sink error")
	}
}

func TestAppendStreamItemNoSink(t *testing.T) {
	store := &mockEventAppender{record: domain.EventRecord{Sequence: 1}}
	item := domain.StreamItem{RunID: "run_1", Kind: domain.StreamKindRunCompleted}
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
		item     domain.StreamItem
		wantKind string
	}{
		{
			name:     "nil_payload",
			item:     domain.StreamItem{Kind: domain.StreamKindRunCompleted},
			wantKind: "run.completed",
		},
		{
			name:     "run_started",
			item:     domain.StreamItem{Kind: domain.StreamKindRunStarted, Payload: map[string]any{"input": "hello"}},
			wantKind: "run.started",
		},
		{
			name:     "tool_call_started",
			item:     domain.StreamItem{Kind: domain.StreamKindToolCallStarted, Payload: map[string]any{"tool_call": &domain.StreamToolCall{Name: "t1", CallID: "c1"}}},
			wantKind: "tool.call.started",
		},
		{
			name:     "assistant_message",
			item:     domain.StreamItem{Kind: domain.StreamKindAssistantMessage, Payload: map[string]any{"message": &domain.StreamMessage{Content: "hi"}}},
			wantKind: "agent.message",
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
		kind domain.StreamItemKind
		want string
	}{
		{domain.StreamKindRunStarted, "run.started"},
		{domain.StreamKindRunCompleted, "run.completed"},
		{domain.StreamKindRunFailed, "run.failed"},
		{domain.StreamKindRunInterrupted, "run.interrupted"},
		{domain.StreamKindRunResumeRequested, "run.resume_requested"},
		{domain.StreamKindAssistantDelta, "assistant.delta"},
		{domain.StreamKindAssistantMessage, "agent.message"},
		{domain.StreamKindToolCallStarted, "tool.call.started"},
		{domain.StreamKindToolCallSucceeded, "tool.call.succeeded"},
		{domain.StreamKindToolCallFailed, "tool.call.failed"},
		{domain.StreamKindToolCallInterrupted, "tool.call.interrupted"},
		{domain.StreamKindSkillDiscovered, "skill.discovered"},
		{domain.StreamKindSkillSelected, "skill.selected"},
		{domain.StreamKindSkillLoaded, "skill.loaded"},
		{domain.StreamKindSkillFailed, "skill.failed"},
		{domain.StreamKindProcedureActivation, "procedure.activation"},
		{domain.StreamKindMemoryPrepared, "memory.prepared"},
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
