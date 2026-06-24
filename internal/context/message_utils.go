package context

import (
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const TurnIndexExtraKey = "acorn_turn_index"

func CloneMessage(msg adk.Message) *schema.Message {
	message := *msg
	if msg.Extra != nil {
		message.Extra = CloneAnyMap(msg.Extra)
	}
	if msg.UserInputMultiContent != nil {
		message.UserInputMultiContent = append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...)
		for i := range message.UserInputMultiContent {
			if message.UserInputMultiContent[i].Extra != nil {
				message.UserInputMultiContent[i].Extra = CloneAnyMap(message.UserInputMultiContent[i].Extra)
			}
		}
	}
	if msg.AssistantGenMultiContent != nil {
		message.AssistantGenMultiContent = append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...)
		for i := range message.AssistantGenMultiContent {
			if message.AssistantGenMultiContent[i].Extra != nil {
				message.AssistantGenMultiContent[i].Extra = CloneAnyMap(message.AssistantGenMultiContent[i].Extra)
			}
		}
	}
	if msg.ToolCalls != nil {
		message.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
	}
	return &message
}

func CloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
