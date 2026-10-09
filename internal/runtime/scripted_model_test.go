package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// scriptedModel replays fixed assistant replies and records every input.
type scriptedModel struct {
	mu      sync.Mutex
	replies []*schema.AgenticMessage
	inputs  [][]*schema.AgenticMessage
}

func (m *scriptedModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...einomodel.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs = append(m.inputs, input)
	if len(m.inputs) > len(m.replies) {
		return nil, fmt.Errorf("scripted model exhausted after %d replies", len(m.replies))
	}
	return m.replies[len(m.inputs)-1], nil
}

func (m *scriptedModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...einomodel.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.AgenticMessage{msg}), nil
}

func (m *scriptedModel) lastInput() []*schema.AgenticMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inputs) == 0 {
		return nil
	}
	return m.inputs[len(m.inputs)-1]
}

func toolCallReply(callID, name, args string) *schema.AgenticMessage {
	return assistantMessage("", []*schema.FunctionToolCall{{CallID: callID, Name: name, Arguments: args}})
}

func assistantMessage(text string, calls []*schema.FunctionToolCall) *schema.AgenticMessage {
	msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}
	if text != "" {
		msg.ContentBlocks = append(msg.ContentBlocks, &schema.ContentBlock{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: text}})
	}
	for _, call := range calls {
		msg.ContentBlocks = append(msg.ContentBlocks, &schema.ContentBlock{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: call})
	}
	return msg
}

func toolResultMessage(text, id, name string) *schema.AgenticMessage {
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{{Type: schema.ContentBlockTypeFunctionToolResult, FunctionToolResult: &schema.FunctionToolResult{
		CallID: id, Name: name, Content: []*schema.FunctionToolResultContentBlock{{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: text}}},
	}}}}
}

func messageText(msg *schema.AgenticMessage) string {
	text := StreamMessageFromSchema(msg, "").Content
	for _, block := range msg.ContentBlocks {
		if block.FunctionToolResult != nil {
			for _, part := range block.FunctionToolResult.Content {
				if part.Text != nil {
					text += part.Text.Text
				}
			}
		}
	}
	return text
}

func toolResultID(msg *schema.AgenticMessage) string {
	for _, block := range msg.ContentBlocks {
		if block.FunctionToolResult != nil {
			return block.FunctionToolResult.CallID
		}
	}
	return ""
}

func eventFromMessage(msg *schema.AgenticMessage, stream *schema.StreamReader[*schema.AgenticMessage], role schema.RoleType, _ string) *adk.TypedAgentEvent[*schema.AgenticMessage] {
	agenticRole := schema.AgenticRoleTypeAssistant
	if role == schema.Tool {
		agenticRole = schema.AgenticRoleTypeUser
	}
	return &adk.TypedAgentEvent[*schema.AgenticMessage]{Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
		Message: msg, MessageStream: stream, IsStreaming: stream != nil, AgenticRole: agenticRole,
	}}}
}
