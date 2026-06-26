package runtime

import (
	"context"
	"errors"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// samplingTestModel is a minimal BaseChatModel that returns a canned response
// from Generate, recording the inputs it received.
type samplingTestModel struct {
	resp   *schema.Message
	err    error
	inputs [][]*schema.Message
}

func (m *samplingTestModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.inputs = append(m.inputs, input)
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

func (m *samplingTestModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not supported")
}

func TestSamplingExecutorAdapterReturnsContent(t *testing.T) {
	model := &samplingTestModel{resp: schema.AssistantMessage("hello world", nil)}
	adapter := samplingExecutorAdapter{model: model}

	out, err := adapter.ExecuteMessages(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("ExecuteMessages failed: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("output = %q, want %q", out, "hello world")
	}
	if len(model.inputs) != 1 || model.inputs[0][0].Content != "hi" {
		t.Fatalf("model received wrong input: %+v", model.inputs)
	}
}

func TestSamplingExecutorAdapterPropagatesError(t *testing.T) {
	model := &samplingTestModel{err: errors.New("model unavailable")}
	adapter := samplingExecutorAdapter{model: model}

	_, err := adapter.ExecuteMessages(context.Background(), nil)
	if err == nil || !errors.Is(err, model.err) {
		t.Fatalf("expected error propagation, got: %v", err)
	}
}
