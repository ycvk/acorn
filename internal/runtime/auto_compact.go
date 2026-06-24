package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	autoCompactMaxFailures   = 3
	autoCompactSummaryPrompt = "Summarize the conversation so far, preserving key decisions, facts, and pending work. Be concise."
)

// autoCompactor generates a conversation summary via a model call, then
// replaces old messages with [summary + recent turns]. A circuit breaker
// stops further compaction attempts after autoCompactMaxFailures consecutive
// failures.
type autoCompactor struct {
	model               einomodel.BaseChatModel
	tokenCounter        TokenCounter
	preserveRecentTurns int
	failures            int
}

func newAutoCompactor(model einomodel.BaseChatModel, counter TokenCounter, preserveRecentTurns int) *autoCompactor {
	return &autoCompactor{
		model:               model,
		tokenCounter:        counter,
		preserveRecentTurns: preserveRecentTurns,
	}
}

// compact splits messages into old (to summarize) and recent (to keep),
// generates a summary, and returns [summary message + recent messages].
// On failure, returns the original messages and a non-nil error; the circuit
// breaker increments and will short-circuit after maxFailures.
func (c *autoCompactor) compact(ctx context.Context, messages []adk.Message) ([]adk.Message, error) {
	if c.failures >= autoCompactMaxFailures {
		return messages, nil // circuit breaker tripped
	}
	preserve := c.preserveRecentTurns
	if preserve <= 0 {
		preserve = 3
	}
	// Each turn is approximately user + assistant (2 messages). Keep the
	// last preserve*2 messages as recent context.
	recentCount := preserve * 2
	if recentCount >= len(messages) {
		return messages, nil // nothing to compact
	}
	splitAt := len(messages) - recentCount
	oldMessages := messages[:splitAt]
	recentMessages := messages[splitAt:]

	summary, err := c.generateSummary(ctx, oldMessages)
	if err != nil {
		c.failures++
		return messages, fmt.Errorf("auto-compact summary generation: %w", err)
	}
	c.failures = 0 // reset on success

	summaryMsg := adk.Message(schema.SystemMessage("Conversation summary:\n" + summary))
	result := make([]adk.Message, 0, len(recentMessages)+1)
	result = append(result, summaryMsg)
	result = append(result, recentMessages...)
	return result, nil
}

func (c *autoCompactor) generateSummary(ctx context.Context, messages []adk.Message) (string, error) {
	serialized := serializeMessagesForSummary(messages)
	prompt := autoCompactSummaryPrompt + "\n\n---\n\n" + serialized
	req := []*schema.Message{schema.UserMessage(prompt)}
	resp, err := c.model.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("auto-compact model returned nil response")
	}
	return strings.TrimSpace(resp.Content), nil
}

func serializeMessagesForSummary(messages []adk.Message) string {
	var b strings.Builder
	for _, m := range messages {
		if m == nil {
			continue
		}
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
