package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/eventstream"
)

type assistantStreamingModel struct {
	stream       *schema.StreamReader[*schema.Message]
	err          error
	lastToolInfo []*schema.ToolInfo
}

type assistantStreamAppenderSpy struct {
	records []domain.EventRecord
}

func (s *assistantStreamAppenderSpy) AppendEvent(runID, kind string, payload any) (domain.EventRecord, error) {
	return s.AppendEventContext(context.Background(), runID, kind, payload)
}

func (s *assistantStreamAppenderSpy) AppendEventContext(_ context.Context, runID, kind string, payload any) (domain.EventRecord, error) {
	record := domain.EventRecord{
		RunID:     runID,
		Kind:      kind,
		Payload:   payload,
		Sequence:  int64(len(s.records) + 1),
		CreatedAt: time.Now().UTC(),
	}
	s.records = append(s.records, record)
	return record, nil
}

func (m *assistantStreamingModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("generate should not be used in assistant stream tests")
}

func (m *assistantStreamingModel) Stream(_ context.Context, _ []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	options := einomodel.GetCommonOptions(nil, opts...)
	m.lastToolInfo = append([]*schema.ToolInfo(nil), options.Tools...)
	return m.stream, m.err
}

func TestStreamAssistantMessageEmitsDeltaItemsAndReturnsFinalMessage(t *testing.T) {
	reader := schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage("你", nil),
		schema.AssistantMessage("好", nil),
	})
	model := &assistantStreamingModel{stream: reader}
	var items []eventstream.StreamItem

	result, err := streamAssistantMessage(context.Background(), model, []*schema.Message{schema.UserMessage("hi")}, assistantStreamOptions{
		MessageID: "run_1:assistant:0",
		RunID:     "run_1",
		Sink: func(item eventstream.StreamItem) error {
			items = append(items, item)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("streamAssistantMessage: %v", err)
	}
	msg := result.Message
	if msg == nil || msg.Content != "你好" {
		t.Fatalf("final message = %#v, want content 你好", msg)
	}
	if result.StopReason != AssistantStopReasonEndTurn {
		t.Fatalf("stop reason = %q, want end_turn", result.StopReason)
	}
	if len(items) != 2 {
		t.Fatalf("delta item count = %d, want 2", len(items))
	}
	if items[0].Kind != eventstream.StreamKindAssistantDelta || items[1].Kind != eventstream.StreamKindAssistantDelta {
		t.Fatalf("unexpected kinds: %#v", items)
	}
	first := items[0].GetAssistantDelta()
	second := items[1].GetAssistantDelta()
	if first == nil || second == nil {
		t.Fatalf("expected assistant deltas, got %#v", items)
	}
	if first.Delta != "你" || first.Sequence != 1 {
		t.Fatalf("first delta = %#v, want delta 你 seq 1", first)
	}
	if second.Delta != "好" || second.Sequence != 2 {
		t.Fatalf("second delta = %#v, want delta 好 seq 2", second)
	}
}

func TestStreamAssistantMessageReturnsErrorForEmptyStream(t *testing.T) {
	reader := schema.StreamReaderFromArray([]*schema.Message{})
	model := &assistantStreamingModel{stream: reader}

	_, err := streamAssistantMessage(context.Background(), model, []*schema.Message{schema.UserMessage("hi")}, assistantStreamOptions{
		MessageID: "run_1:assistant:0",
		RunID:     "run_1",
	})
	if err == nil || err.Error() != "assistant stream returned no frames" {
		t.Fatalf("err = %v, want empty stream error", err)
	}
}

func TestStreamAssistantMessageAppendsDeltaWithoutLiveSink(t *testing.T) {
	reader := schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage("Budget exhausted.", nil),
	})
	model := &assistantStreamingModel{stream: reader}
	appender := &assistantStreamAppenderSpy{}

	result, err := streamAssistantMessage(context.Background(), model, []*schema.Message{schema.UserMessage("hi")}, assistantStreamOptions{
		MessageID: "run_1:assistant:0",
		RunID:     "run_1",
		Appender:  appender,
	})
	if err != nil {
		t.Fatalf("streamAssistantMessage: %v", err)
	}
	msg := result.Message
	if msg == nil || msg.Content != "Budget exhausted." {
		t.Fatalf("final message = %#v, want streamed content", msg)
	}
	if len(appender.records) != 1 {
		t.Fatalf("appended record count = %d, want 1", len(appender.records))
	}
	item := projectEventToStreamItem(appender.records[0])
	delta := item.GetAssistantDelta()
	if item.Kind != eventstream.StreamKindAssistantDelta || delta == nil {
		t.Fatalf("appended item = %#v, want assistant delta", item)
	}
	if delta.Delta != "Budget exhausted." || delta.Sequence != 1 {
		t.Fatalf("delta = %#v, want streamed delta seq 1", delta)
	}
}

func TestDirectAssistantStreamerPersistsAndSinksDeltas(t *testing.T) {
	reader := schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage("he", nil),
		schema.AssistantMessage("llo", nil),
	})
	model := &assistantStreamingModel{stream: reader}
	appender := &assistantStreamAppenderSpy{}
	streamer := NewDirectAssistantStreamer(appender)
	var sinkItems []eventstream.StreamItem
	ctx := eventstream.WithStreamSink(context.Background(), func(item eventstream.StreamItem) error {
		sinkItems = append(sinkItems, item)
		return nil
	})
	toolInfo := &schema.ToolInfo{Name: "lookup"}

	result, err := streamer.StreamAssistantMessage(ctx, orchestrationAssistantStreamRequestForTest("run_stream", model, toolInfo))
	if err != nil {
		t.Fatalf("StreamAssistantMessage: %v", err)
	}
	msg := result.Message
	if msg == nil || msg.Content != "hello" {
		t.Fatalf("final message = %#v, want hello", msg)
	}
	if len(appender.records) != 2 {
		t.Fatalf("persisted delta count = %d, want 2", len(appender.records))
	}
	if len(sinkItems) != 2 {
		t.Fatalf("sink delta count = %d, want 2", len(sinkItems))
	}
	first := projectEventToStreamItem(appender.records[0]).GetAssistantDelta()
	second := projectEventToStreamItem(appender.records[1]).GetAssistantDelta()
	if first == nil || second == nil {
		t.Fatalf("persisted events are not assistant deltas: %#v", appender.records)
	}
	if first.MessageID != "run_stream:assistant:0" || first.Delta != "he" || first.Sequence != 1 {
		t.Fatalf("first delta = %#v, want message id and first chunk", first)
	}
	if second.MessageID != "run_stream:assistant:0" || second.Delta != "llo" || second.Sequence != 2 {
		t.Fatalf("second delta = %#v, want message id and second chunk", second)
	}
	if len(model.lastToolInfo) != 1 || model.lastToolInfo[0].Name != "lookup" {
		t.Fatalf("stream tools = %#v, want lookup", model.lastToolInfo)
	}
}

func orchestrationAssistantStreamRequestForTest(runID string, model *assistantStreamingModel, toolInfo *schema.ToolInfo) AssistantStreamRequest {
	return AssistantStreamRequest{
		RunID:     runID,
		MessageID: runID + ":assistant:0",
		Model:     model,
		Messages:  []*schema.Message{schema.UserMessage("hi")},
		ToolInfos: []*schema.ToolInfo{toolInfo},
	}
}

func TestRunStateApplyStreamItemAppendsAssistantDelta(t *testing.T) {
	var state runState
	state.applyStreamItem(eventstream.StreamItem{
		Kind: eventstream.StreamKindAssistantDelta,
		Payload: map[string]any{"assistant_delta": &eventstream.StreamAssistantDelta{
			Delta:    "partial ",
			Sequence: 1,
		}},
	})
	state.applyStreamItem(eventstream.StreamItem{
		Kind: eventstream.StreamKindAssistantDelta,
		Payload: map[string]any{"assistant_delta": &eventstream.StreamAssistantDelta{
			Delta:    "answer",
			Sequence: 2,
		}},
	})
	if state.lastOutput != "partial answer" {
		t.Fatalf("lastOutput = %q, want %q", state.lastOutput, "partial answer")
	}
}

func projectEventToStreamItem(event domain.EventRecord) eventstream.StreamItem {
	item := eventstream.StreamItem{
		RunID:     event.RunID,
		Kind:      eventstream.StreamItemKind(event.Kind),
		CreatedAt: event.CreatedAt,
	}
	data, _ := json.Marshal(event.Payload)
	var payload map[string]any
	json.Unmarshal(data, &payload)
	if payload != nil {
		item.Payload = payload
	}
	return item
}

type runState struct {
	lastOutput string
}

func (s *runState) applyStreamItem(item eventstream.StreamItem) {
	if delta := item.GetAssistantDelta(); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := item.GetMessage(); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
}
