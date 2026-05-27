package compaction

import (
	"errors"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
)

func buildSummarizerInput(previousSummary string, messages []adk.Message, preservePolicy contextplane.PreservePolicy) ([]adk.Message, error) {
	transcript := buildCompressionTranscript(compressionCandidateSlice(messages, preservePolicy))
	if strings.TrimSpace(transcript) == "" {
		return nil, errors.New("compression transcript is empty")
	}
	return []adk.Message{
		&schema.Message{
			Role:    schema.System,
			Content: summarizerSystemPrompt,
		},
		&schema.Message{
			Role:    schema.User,
			Content: buildSummarizerUserPrompt(contextplane.RedactSecrets(previousSummary), transcript),
		},
	}, nil
}

func compressionCandidateSlice(messages []adk.Message, preservePolicy contextplane.PreservePolicy) []adk.Message {
	firstIndex, lastIndex := compressionRewriteRange(messages, preservePolicy)
	if firstIndex < 0 || lastIndex < firstIndex {
		return nil
	}
	return append([]adk.Message(nil), messages[firstIndex:lastIndex+1]...)
}

func compressionRewriteRange(messages []adk.Message, preservePolicy contextplane.PreservePolicy) (int, int) {
	systemCount := len(messages) - len(stripLeadingSystemMessages(messages))
	contextMessages := messages[systemCount:]
	if len(contextMessages) == 0 {
		return -1, -1
	}
	recentTail := preservedConversationTail(contextMessages, preservePolicy)
	lastIndex := len(messages) - len(recentTail) - 1
	if lastIndex < systemCount {
		return -1, -1
	}
	return systemCount, lastIndex
}

func stripLeadingSystemMessages(messages []adk.Message) []adk.Message {
	index := 0
	for index < len(messages) {
		if messages[index] == nil || messages[index].Role != schema.System {
			break
		}
		index++
	}
	return append([]adk.Message(nil), messages[index:]...)
}

func preservedConversationTail(messages []adk.Message, preservePolicy contextplane.PreservePolicy) []adk.Message {
	if len(messages) == 0 || preservePolicy.RecentTurns <= 0 {
		return nil
	}
	turnsSeen := 0
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] == nil || messages[i].Role != schema.User {
			continue
		}
		turnsSeen++
		start = i
		if turnsSeen >= preservePolicy.RecentTurns {
			break
		}
	}
	if turnsSeen == 0 {
		start = max(0, len(messages)-preservePolicy.RecentTurns)
	}
	if preservePolicy.PreserveToolPairs {
		start = expandTailStartForToolPairs(messages, start)
	}
	return contextplane.CloneContextSessionMessages(messages[start:])
}

func expandTailStartForToolPairs(messages []adk.Message, start int) int {
	if start <= 0 || start >= len(messages) {
		return max(0, min(start, len(messages)))
	}
	expanded := start
	for i := start; i < len(messages); i++ {
		msg := messages[i]
		if msg == nil || msg.Role != schema.Tool {
			continue
		}
		assistantIndex := assistantToolCallIndex(messages[:start], msg.ToolCallID)
		if assistantIndex >= 0 && assistantIndex < expanded {
			expanded = assistantIndex
		}
		if msg.ToolCallID == "" && start > 0 && messages[start-1] != nil && messages[start-1].Role == schema.Assistant && len(messages[start-1].ToolCalls) > 0 && start-1 < expanded {
			expanded = start - 1
		}
	}
	return expanded
}

func assistantToolCallIndex(messages []adk.Message, toolCallID string) int {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		for _, call := range msg.ToolCalls {
			if toolCallID != "" && call.ID == toolCallID {
				return i
			}
		}
		if toolCallID == "" && len(msg.ToolCalls) > 0 {
			return i
		}
	}
	return -1
}

func buildCompressionTranscript(messages []adk.Message) string {
	var b strings.Builder
	for _, msg := range messages {
		if msg == nil || isCompressionSummary(msg) {
			continue
		}
		role := string(msg.Role)
		if role == "" {
			continue
		}
		content := strings.TrimSpace(messageText(msg))
		if content == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(role)
		b.WriteString("]: ")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func messageText(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	var parts []string
	if text := strings.TrimSpace(msg.Content); text != "" {
		parts = append(parts, text)
	}
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	for _, part := range msg.AssistantGenMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
		if part.Type == schema.ChatMessagePartTypeReasoning && part.Reasoning != nil && strings.TrimSpace(part.Reasoning.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Reasoning.Text))
		}
	}
	for _, call := range msg.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			name = strings.TrimSpace(call.ID)
		}
		if name == "" {
			continue
		}
		if args := strings.TrimSpace(call.Function.Arguments); args != "" {
			parts = append(parts, "tool_call: "+name+" "+args)
		} else {
			parts = append(parts, "tool_call: "+name)
		}
	}
	return strings.Join(parts, "\n")
}

func summaryMessageText(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			return strings.TrimSpace(part.Text)
		}
	}
	return messageText(msg)
}

func isCompressionSummary(msg adk.Message) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	value, ok := msg.Extra[contextplane.CompressionSummaryMarkerKey].(string)
	return ok && value == contextplane.CompressionSummaryMarkerValue
}
