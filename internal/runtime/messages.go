package runtime

import (
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// StreamMessageFromSchema projects public content only. Provider signatures and
// opaque reasoning remain in Eino state and never enter the client event stream.
func StreamMessageFromSchema(message *schema.AgenticMessage, activeProvider string) *core.StreamMessage {
	if message == nil {
		return nil
	}
	stream := &core.StreamMessage{Role: string(message.Role)}
	var content, reasoning strings.Builder
	for _, block := range message.ContentBlocks {
		switch block.Type {
		case schema.ContentBlockTypeAssistantGenText:
			content.WriteString(block.AssistantGenText.Text)
		case schema.ContentBlockTypeUserInputText:
			content.WriteString(block.UserInputText.Text)
		case schema.ContentBlockTypeReasoning:
			reasoning.WriteString(block.Reasoning.Text)
		case schema.ContentBlockTypeFunctionToolCall:
			call := block.FunctionToolCall
			stream.ToolCalls = append(stream.ToolCalls, core.StreamPlannedToolCall{ID: call.CallID, Name: call.Name, ArgumentsJSON: call.Arguments})
		}
	}
	stream.Content = content.String()
	stream.Reasoning = reasoning.String()
	if activeProvider != "" {
		stream.Meta = map[string]any{"active_provider": activeProvider}
	}
	return stream
}

func streamMessageMeta(message *schema.AgenticMessage) map[string]any {
	if message == nil || message.ResponseMeta == nil {
		return nil
	}
	meta := message.ResponseMeta
	if ext := meta.ClaudeExtension; ext != nil && ext.StopReason != "" {
		return map[string]any{"finish_reason": ext.StopReason}
	}
	if ext := meta.OpenAIExtension; ext != nil && ext.Status != "" {
		return map[string]any{"finish_reason": string(ext.Status)}
	}
	return nil
}
