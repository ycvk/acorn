package app

import (
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/events"
)

const chatHistoryLimit = 12

func buildChatMessages(items []events.SessionMessageRecord) []adk.Message {
	messages := make([]adk.Message, 0, len(items))
	for _, item := range items {
		switch item.Role {
		case "user":
			messages = append(messages, schema.UserMessage(item.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(item.Content, nil))
		}
	}
	return messages
}
