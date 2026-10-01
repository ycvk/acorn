package runtime

import (
	"context"
	"fmt"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// scriptedModel replays fixed assistant replies and records every input.
type scriptedModel struct {
	mu      sync.Mutex
	replies []*schema.Message
	inputs  [][]*schema.Message
}

func (m *scriptedModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs = append(m.inputs, input)
	if len(m.inputs) > len(m.replies) {
		return nil, fmt.Errorf("scripted model exhausted after %d replies", len(m.replies))
	}
	return m.replies[len(m.inputs)-1], nil
}

func (m *scriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *scriptedModel) lastInput() []*schema.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inputs) == 0 {
		return nil
	}
	return m.inputs[len(m.inputs)-1]
}

func toolCallReply(callID, name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID:       callID,
		Type:     "function",
		Function: schema.FunctionCall{Name: name, Arguments: args},
	}})
}
