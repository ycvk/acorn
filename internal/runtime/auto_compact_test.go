package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestAutoCompactorReplacesOldMessagesWithSummary(t *testing.T) {
	counter := testTokenCounter(t)
	model := &stubSummaryModel{response: "summary of conversation"}
	c := newAutoCompactor(model, counter, 1)

	msgs := []adk.Message{
		schema.UserMessage("old request 1"),
		schema.AssistantMessage("old response 1", nil),
		schema.UserMessage("recent request"),
	}
	result, err := c.compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	// preserveRecentTurns=1 => keep last 1*2=2 messages; 3 msgs total =>
	// splitAt=1, old=[msg0], recent=[msg1,msg2]. Result: [summary, msg1, msg2] = 3.
	if len(result) != 3 {
		t.Fatalf("result length = %d, want 3", len(result))
	}
	if !strings.Contains(result[0].Content, "summary of conversation") {
		t.Fatalf("summary content = %q, want contains summary", result[0].Content)
	}
	if result[1].Content != "old response 1" {
		t.Fatalf("first recent message = %q, want 'old response 1'", result[1].Content)
	}
	if result[2].Content != "recent request" {
		t.Fatalf("last recent message = %q, want 'recent request'", result[2].Content)
	}
}

func TestAutoCompactorPreservesRecentTurns(t *testing.T) {
	counter := testTokenCounter(t)
	model := &stubSummaryModel{response: "summary"}
	c := newAutoCompactor(model, counter, 2) // preserve last 2*2=4 messages

	msgs := []adk.Message{
		schema.UserMessage("old 1"),
		schema.AssistantMessage("resp 1", nil),
		schema.UserMessage("recent user"),
		schema.AssistantMessage("recent assistant", nil),
		schema.UserMessage("latest user"),
		schema.AssistantMessage("latest assistant", nil),
	}
	result, err := c.compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	// summary + 4 recent messages
	if len(result) != 5 {
		t.Fatalf("result length = %d, want 5 (summary + 4 recent)", len(result))
	}
	if result[0].Role != schema.System {
		t.Fatalf("first message should be summary (system)")
	}
	// last 4 messages should be the recent ones
	if result[1].Content != "recent user" {
		t.Fatalf("first recent = %q, want 'recent user'", result[1].Content)
	}
	if result[4].Content != "latest assistant" {
		t.Fatalf("last recent = %q, want 'latest assistant'", result[4].Content)
	}
}

func TestAutoCompactorReturnsMessagesWhenNothingToCompact(t *testing.T) {
	counter := testTokenCounter(t)
	model := &stubSummaryModel{response: "summary"}
	c := newAutoCompactor(model, counter, 10) // preserve more than we have

	msgs := []adk.Message{
		schema.UserMessage("only message"),
	}
	result, err := c.compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(result) != 1 || result[0].Content != "only message" {
		t.Fatalf("result = %v, want original messages unchanged", result)
	}
}

func TestAutoCompactorCircuitBreakerAfterMaxFailures(t *testing.T) {
	counter := testTokenCounter(t)
	model := &stubErrorModel{}
	c := newAutoCompactor(model, counter, 1)

	msgs := []adk.Message{
		schema.UserMessage("old 1"),
		schema.AssistantMessage("resp 1", nil),
		schema.UserMessage("recent"),
	}
	// Trigger 3 failures to trip the circuit breaker.
	for i := 0; i < autoCompactMaxFailures; i++ {
		_, err := c.compact(context.Background(), msgs)
		if err == nil {
			t.Fatalf("expected error on attempt %d", i)
		}
	}
	if c.failures != autoCompactMaxFailures {
		t.Fatalf("failures = %d, want %d", c.failures, autoCompactMaxFailures)
	}
	// After circuit breaker trips, compact should return messages unchanged.
	result, err := c.compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("circuit breaker compact should not error: %v", err)
	}
	if len(result) != len(msgs) {
		t.Fatalf("circuit breaker should return original messages, got %d", len(result))
	}
}

// --- helpers ---

type stubSummaryModel struct {
	response string
}

func (m *stubSummaryModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.response, nil), nil
}

func (m *stubSummaryModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, context.Canceled
}

type stubErrorModel struct{}

func (m *stubErrorModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return nil, context.DeadlineExceeded
}

func (m *stubErrorModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, context.Canceled
}
