package stream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

// mockEventAppender implements api.EventAppender for testing.
type mockEventAppender struct {
	record events.EventRecord
	err    error
}

func (m *mockEventAppender) AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error) {
	return m.record, m.err
}

func TestAppendStreamItemNilStore(t *testing.T) {
	item := StreamItem{RunID: "run_1", Kind: StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), nil, nil, item)
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestAppendStreamItemSuccess(t *testing.T) {
	now := time.Now()
	store := &mockEventAppender{
		record: events.EventRecord{
			Sequence:  42,
			RunID:     "run_1",
			Kind:      "run.started",
			CreatedAt: now,
		},
	}

	var sinkCalled bool
	sink := func(item StreamItem) error {
		sinkCalled = true
		if item.Sequence != 42 {
			t.Fatalf("sink: sequence = %d, want 42", item.Sequence)
		}
		return nil
	}

	item := StreamItem{RunID: "run_1", Kind: StreamKindRunStarted, Payload: RunStartedPayload{Input: "hello"}}
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
	item := StreamItem{RunID: "run_1", Kind: StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), store, nil, item)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAppendStreamItemSinkError(t *testing.T) {
	store := &mockEventAppender{record: events.EventRecord{Sequence: 1}}
	sink := func(item StreamItem) error { return errors.New("sink error") }
	item := StreamItem{RunID: "run_1", Kind: StreamKindRunStarted}
	_, err := AppendStreamItem(context.Background(), store, sink, item)
	if err == nil {
		t.Fatal("expected sink error")
	}
}

func TestAppendStreamItemNoSink(t *testing.T) {
	store := &mockEventAppender{record: events.EventRecord{Sequence: 1}}
	item := StreamItem{RunID: "run_1", Kind: StreamKindHeartbeat}
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
		item     StreamItem
		wantKind string
	}{
		{
			name:     "nil_payload",
			item:     StreamItem{Kind: StreamKindHeartbeat},
			wantKind: "stream.heartbeat",
		},
		{
			name:     "run_started",
			item:     StreamItem{Kind: StreamKindRunStarted, Payload: RunStartedPayload{Input: "hello"}},
			wantKind: "run.started",
		},
		{
			name:     "tool_call_started",
			item:     StreamItem{Kind: StreamKindToolCallStarted, Payload: ToolCallStartedPayload{ToolCall: &StreamToolCall{Name: "t1", CallID: "c1"}}},
			wantKind: "tool.call.started",
		},
		{
			name:     "assistant_message",
			item:     StreamItem{Kind: StreamKindAssistantMessage, Payload: AssistantMessagePayload{Message: &StreamMessage{Content: "hi"}}},
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
		kind StreamItemKind
		want string
	}{
		{StreamKindRunStarted, "run.started"},
		{StreamKindRunCompleted, "run.completed"},
		{StreamKindRunFailed, "run.failed"},
		{StreamKindRunInterrupted, "run.interrupted"},
		{StreamKindRunResumeRequested, "run.resume_requested"},
		{StreamKindAssistantDelta, "assistant.delta"},
		{StreamKindAssistantMessage, "agent.message"},
		{StreamKindToolCallStarted, "tool.call.started"},
		{StreamKindToolCallProgress, "tool.call.progress"},
		{StreamKindToolCallSucceeded, "tool.call.succeeded"},
		{StreamKindToolCallFailed, "tool.call.failed"},
		{StreamKindToolCallInterrupted, "tool.call.interrupted"},
		{StreamKindSkillDiscovered, "skill.discovered"},
		{StreamKindSkillSelected, "skill.selected"},
		{StreamKindSkillLoaded, "skill.loaded"},
		{StreamKindSkillFailed, "skill.failed"},
		{StreamKindSkillLifecycle, "skill.lifecycle"},
		{StreamKindProcedureActivation, "procedure.activation"},
		{StreamKindMemoryPrepared, "memory.prepared"},
		{StreamKindContextPressure, "context.pressure"},
		{StreamKindContextCompressed, "context.compressed"},
		{StreamKindHeartbeat, "stream.heartbeat"},
		{StreamKindToolParallelBatchStarted, "tool.parallel_batch.started"},
		{StreamKindToolParallelBatchCompleted, "tool.parallel_batch.completed"},
		{StreamKindRunArchived, "run.archived"},
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
