package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	autoCompactMaxFailures   = 3
	autoCompactSummaryPrompt = "Summarize the conversation so far, preserving key decisions, facts, and pending work. Be concise."
)

// autoCompactor generates a conversation summary via a background model call,
// then replaces the summarized messages with a single summary message between
// turns. A circuit breaker stops further compaction attempts after
// autoCompactMaxFailures consecutive failures.
//
// The compactor only ever sees the conversation zone of a session (everything
// after the bootstrap prefix of system instruction + assembled context), so
// the prefix is never summarized away.
//
// Concurrency: maybeStartCompact launches a background goroutine that only
// reads a private copy of the compact zone and writes the summary into
// pendingCompact. applyPendingCompact is called from the same single goroutine
// that owns the session (BeforeModelCall), so the session messages stay
// single-writer. The mutex protects only the pending/failures state.
type autoCompactor struct {
	model               einomodel.BaseChatModel
	tokenCounter        TokenCounter
	preserveRecentTurns int
	failures            int

	mu      sync.Mutex
	pending *pendingCompact
}

// pendingCompact holds the state of a background summary generation.
type pendingCompact struct {
	// splitAt is the index in the conversation at which the compact zone ends.
	// The conversation only grows by appending between start and apply, so
	// conversation[:splitAt] is exactly what the summary replaces.
	splitAt int
	done    chan struct{}
	summary string
	failed  bool
}

func newAutoCompactor(model einomodel.BaseChatModel, counter TokenCounter, preserveRecentTurns int) *autoCompactor {
	return &autoCompactor{
		model:               model,
		tokenCounter:        counter,
		preserveRecentTurns: preserveRecentTurns,
	}
}

// maybeStartCompact starts a background summary generation for the compact
// zone of conversation and returns immediately. If a compaction is already in
// flight or the circuit breaker has tripped, it is a no-op. The summary is
// spliced in between turns by applyPendingCompact.
func (c *autoCompactor) maybeStartCompact(ctx context.Context, conversation []adk.Message) {
	c.mu.Lock()
	// If a previous compaction settled (done), clear it so a new one can
	// start. This matters for the failure path: a failed compaction must not
	// block subsequent attempts until the circuit breaker trips.
	if c.pending != nil {
		select {
		case <-c.pending.done:
			c.pending = nil
		default:
			// still in flight; cannot start a new one
		}
	}
	if c.pending != nil || c.failures >= autoCompactMaxFailures {
		c.mu.Unlock()
		return
	}
	splitAt := c.splitPoint(conversation)
	if splitAt < 0 {
		c.mu.Unlock()
		return
	}
	oldMessages := append([]adk.Message(nil), conversation[:splitAt]...)
	p := &pendingCompact{splitAt: splitAt, done: make(chan struct{})}
	c.pending = p
	c.mu.Unlock()

	go func() {
		// Use a detached context so the summary outlives the run that
		// started it. The compaction is between-turn work — it must not be
		// cancelled when the run's context expires, or every run that hits
		// the threshold near its end would spuriously trip the circuit breaker.
		summaryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		summary, err := c.generateSummary(summaryCtx, oldMessages)
		c.mu.Lock()
		if err != nil {
			c.failures++
			p.failed = true
		} else {
			c.failures = 0
			p.summary = summary
		}
		close(p.done)
		c.mu.Unlock()
	}()
}

// applyPendingCompact checks whether a background summary has completed. If it
// has, it returns [summary message + conversation[splitAt:]] and clears the
// pending state. If no compaction is pending, it has not completed yet, or it
// failed, conversation is returned unchanged.
func (c *autoCompactor) applyPendingCompact(conversation []adk.Message) ([]adk.Message, error) {
	c.mu.Lock()
	p := c.pending
	if p == nil {
		c.mu.Unlock()
		return conversation, nil
	}
	select {
	case <-p.done:
		c.pending = nil
		c.mu.Unlock()
	default:
		c.mu.Unlock()
		return conversation, nil
	}
	if p.failed || p.summary == "" {
		return conversation, nil
	}
	if p.splitAt > len(conversation) {
		return nil, fmt.Errorf("auto-compact split %d exceeds conversation length %d", p.splitAt, len(conversation))
	}
	summaryMsg := adk.Message(schema.SystemMessage("Conversation summary:\n" + p.summary))
	result := make([]adk.Message, 0, len(conversation)-p.splitAt+1)
	result = append(result, summaryMsg)
	result = append(result, conversation[p.splitAt:]...)
	return result, nil
}

// splitPoint returns the index dividing conversation into a compact zone (to
// summarize) and a live zone (kept verbatim), or -1 when there is nothing to
// compact. The live zone never starts with a tool result: that would orphan it
// from the assistant tool call it answers, which providers reject.
func (c *autoCompactor) splitPoint(conversation []adk.Message) int {
	preserve := c.preserveRecentTurns
	if preserve <= 0 {
		preserve = 3
	}
	splitAt := len(conversation) - preserve*2
	if splitAt <= 0 {
		return -1
	}
	for splitAt < len(conversation) && conversation[splitAt] != nil && conversation[splitAt].Role == schema.Tool {
		splitAt++
	}
	if splitAt >= len(conversation) {
		return -1
	}
	return splitAt
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
