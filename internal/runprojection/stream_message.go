package runprojection

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

func StreamMessageFromSchema(message *schema.Message, activeProvider string) *StreamMessage {
	if message == nil {
		return nil
	}
	stream := &StreamMessage{
		Role:       string(message.Role),
		Content:    strings.TrimSpace(message.Content),
		Reasoning:  strings.TrimSpace(message.ReasoningContent),
		ToolCallID: message.ToolCallID,
		ToolName:   message.ToolName,
	}
	meta := make(map[string]any)
	if activeProvider != "" {
		meta["active_provider"] = activeProvider
	}
	if len(message.ToolCalls) > 0 {
		stream.ToolCalls = make([]StreamPlannedToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			stream.ToolCalls = append(stream.ToolCalls, StreamPlannedToolCall{
				ID:            call.ID,
				Name:          call.Function.Name,
				ArgumentsJSON: call.Function.Arguments,
			})
		}
	}
	if len(meta) > 0 {
		stream.Meta = meta
	}
	return stream
}
