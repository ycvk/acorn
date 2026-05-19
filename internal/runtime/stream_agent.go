package runtime

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func activeProviderName(chatModel einomodel.BaseChatModel) string {
	if ap, ok := chatModel.(interface{ ActiveProvider() string }); ok {
		return ap.ActiveProvider()
	}
	return ""
}

func streamItemsFromAgentEvent(event *adk.AgentEvent, chatModel einomodel.BaseChatModel) []StreamItem {
	items := make([]StreamItem, 0, 3)
	createdAt := time.Now().UTC()
	if event.Output != nil && event.Output.MessageOutput != nil {
		if message, err := event.Output.MessageOutput.GetMessage(); err == nil && message != nil {
			items = append(items, StreamItem{
				Kind:      StreamKindAssistantMessage,
				CreatedAt: createdAt,
				Payload:   &AssistantMessagePayload{Message: streamMessageFromSchema(message, activeProviderName(chatModel))},
			})
		}
	}
	if event.Action != nil && event.Action.Interrupted != nil {
		items = append(items, StreamItem{
			Kind:      StreamKindRunInterrupted,
			CreatedAt: createdAt,
			Payload:   &RunInterruptedPayload{Interrupt: streamInterruptFromInfo(event.Action.Interrupted)},
		})
	}
	if event.Err != nil {
		items = append(items, StreamItem{
			Kind:      StreamKindRunFailed,
			CreatedAt: createdAt,
			Payload:   &RunFailedPayload{Error: event.Err.Error()},
		})
	}
	return items
}

func streamMessageFromSchema(message *schema.Message, activeProvider string) *StreamMessage {
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

func streamInterruptFromInfo(info *adk.InterruptInfo) *StreamInterrupt {
	if info == nil {
		return nil
	}
	interrupt := &StreamInterrupt{ContextCount: len(info.InterruptContexts), Contexts: make([]StreamInterruptContext, 0, len(info.InterruptContexts))}
	for _, item := range info.InterruptContexts {
		interrupt.Contexts = append(interrupt.Contexts, StreamInterruptContext{
			ID:          item.ID,
			Address:     fmt.Sprint(item.Address),
			Info:        compactInterruptInfo(item.Info),
			IsRootCause: item.IsRootCause,
		})
	}
	return interrupt
}
