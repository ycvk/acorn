package providers

import (
	"context"
	"errors"
	"io"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeRecorder struct {
	records []UsageRecord
	err     error
}

func (r *fakeRecorder) AppendProviderUsage(_ context.Context, record UsageRecord) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, record)
	return nil
}

type usageModel struct {
	generateMessage *schema.Message
	streamMessages  []*schema.Message
}

func (m *usageModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return m.generateMessage, nil
}

func (m *usageModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray(m.streamMessages), nil
}

func TestRecordingModelGeneratePersistsReturnedUsage(t *testing.T) {
	recorder := &fakeRecorder{}
	model, err := WrapModelWithUsage(&usageModel{generateMessage: usageMessage(10, 3, 13, 7, 2)}, recorder, UsageRunMetadata{
		RunID:        "run_1",
		SessionID:    "session_1",
		ProviderName: "openai",
		ModelName:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("WrapModelWithUsage: %v", err)
	}

	msg, err := model.Generate(WithCallSite(context.Background(), CallSitePlan), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		t.Fatalf("expected usage message, got %+v", msg)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v", recorder.records)
	}
	record := recorder.records[0]
	if record.UsageID != "provider_usage:run_1:000001" || record.CallSite != CallSitePlan {
		t.Fatalf("unexpected usage identity: %+v", record)
	}
	if record.PromptTokens != 10 || record.CompletionTokens != 3 || record.TotalTokens != 13 || record.CachedTokens != 7 || record.ReasoningTokens != 2 {
		t.Fatalf("unexpected token usage: %+v", record)
	}
}

func TestRecordingModelRespectsInitialSequence(t *testing.T) {
	recorder := &fakeRecorder{}
	model, err := WrapModelWithUsage(&usageModel{generateMessage: usageMessage(5, 2, 7, 0, 0)}, recorder, UsageRunMetadata{
		RunID:           "run_2",
		SessionID:       "session_1",
		ProviderName:    "openai",
		ModelName:       "gpt-test",
		InitialSequence: 3,
	})
	if err != nil {
		t.Fatalf("WrapModelWithUsage: %v", err)
	}

	msg, err := model.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if msg == nil {
		t.Fatalf("expected message, got nil")
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v", recorder.records)
	}
	record := recorder.records[0]
	if record.UsageID != "provider_usage:run_2:000004" {
		t.Fatalf("unexpected usage id with initial sequence: %+v", record)
	}
}

func TestRecordingModelStreamPersistsUsageAfterEOF(t *testing.T) {
	recorder := &fakeRecorder{}
	model, err := WrapModelWithUsage(&usageModel{streamMessages: []*schema.Message{
		schema.AssistantMessage("hello", nil),
		usageMessage(12, 4, 16, 6, 1),
	}}, recorder, UsageRunMetadata{
		RunID:        "run_stream",
		SessionID:    "session_1",
		ProviderName: "openai",
		ModelName:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("WrapModelWithUsage: %v", err)
	}

	stream, err := model.Stream(WithCallSite(context.Background(), CallSiteAssistant), nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	for {
		_, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv: %v", recvErr)
		}
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v", recorder.records)
	}
	record := recorder.records[0]
	if record.UsageID != "provider_usage:run_stream:000001" || record.CallSite != CallSiteAssistant || record.CachedTokens != 6 {
		t.Fatalf("unexpected streamed usage record: %+v", record)
	}
}

func TestRecordingModelSurfacesRecorderError(t *testing.T) {
	recorder := &fakeRecorder{err: errors.New("store down")}
	model, err := WrapModelWithUsage(&usageModel{generateMessage: usageMessage(1, 1, 2, 0, 0)}, recorder, UsageRunMetadata{
		RunID:        "run_1",
		ProviderName: "openai",
		ModelName:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("WrapModelWithUsage: %v", err)
	}

	_, err = model.Generate(context.Background(), nil)
	if err == nil || err.Error() != "record provider usage provider_usage:run_1:000001: store down" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func usageMessage(prompt, completion, total, cached, reasoning int) *schema.Message {
	msg := schema.AssistantMessage("ok", nil)
	msg.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		PromptTokenDetails: schema.PromptTokenDetails{
			CachedTokens: cached,
		},
		CompletionTokensDetails: schema.CompletionTokensDetails{
			ReasoningTokens: reasoning,
		},
	}}
	return msg
}
