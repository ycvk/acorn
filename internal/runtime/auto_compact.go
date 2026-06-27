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
// then replaces old messages with [summary + recent messages] between turns.
// A circuit breaker stops further compaction attempts after
// autoCompactMaxFailures consecutive failures.
//
// Concurrency: maybeStartCompact launches a background goroutine that only
// reads the snapshot it was given and writes the summary into pendingCompact.
// applyPendingCompact is called from the same single goroutine that owns the
// session (BeforeModelCall), so s.messages is still single-writer. The pending
// mutex protects only the pendingCompact swap.
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
	// splitAt is the index in the snapshot at which the compact zone ends
	// and the live zone begins. Kept for diagnostics.
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

// compact is the legacy synchronous compaction path. It blocks on
// model.Generate. Retained for existing tests; BeforeModelCall now uses the
// non-blocking maybeStartCompact / applyPendingCompact pair.
func (c *autoCompactor) compact(ctx context.Context, messages []adk.Message) ([]adk.Message, error) {
	if c.failures >= autoCompactMaxFailures {
		return messages, nil // circuit breaker tripped
	}
	splitAt, oldMessages, recentMessages := c.splitMessages(messages)
	if splitAt < 0 {
		return messages, nil // nothing to compact
	}

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

// maybeStartCompact starts a background summary generation for the compact
// zone of messages and returns the original messages immediately. If a
// compaction is already in flight or the circuit breaker has tripped, it is a
// no-op. The caller proceeds with its turn; the summary is spliced in between
// turns by applyPendingCompact.
func (c *autoCompactor) maybeStartCompact(ctx context.Context, messages []adk.Message) []adk.Message {
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
		return messages
	}
	splitAt, oldMessages, _ := c.splitMessages(messages)
	if splitAt < 0 {
		c.mu.Unlock()
		return messages
	}
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

	return messages
}

// applyPendingCompact checks whether a background summary has completed. If it
// has, it splices [summary message + live messages] and clears the pending
// state. If no compaction is pending or it has not completed yet, it returns
// live unchanged. Called from the session's single goroutine (BeforeModelCall).
func (c *autoCompactor) applyPendingCompact(live []adk.Message) []adk.Message {
	c.mu.Lock()
	p := c.pending
	if p == nil {
		c.mu.Unlock()
		return live
	}
	select {
	case <-p.done:
		c.pending = nil
		c.mu.Unlock()
	default:
		c.mu.Unlock()
		return live
	}
	if p.failed || p.summary == "" {
		return live
	}
	summaryMsg := adk.Message(schema.SystemMessage("Conversation summary:\n" + p.summary))
	result := make([]adk.Message, 0, len(live)+1)
	result = append(result, summaryMsg)
	result = append(result, live...)
	return result
}

// pendingCompactDone reports whether a background compaction has settled
// (completed or failed). Used by tests; not for production callers.
func (c *autoCompactor) pendingCompactDone() bool {
	c.mu.Lock()
	p := c.pending
	c.mu.Unlock()
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// splitMessages divides messages into a compact zone (to summarize) and a live
// zone (to keep verbatim). Returns splitAt=-1 when there is nothing to compact.
func (c *autoCompactor) splitMessages(messages []adk.Message) (int, []adk.Message, []adk.Message) {
	preserve := c.preserveRecentTurns
	if preserve <= 0 {
		preserve = 3
	}
	recentCount := preserve * 2
	if recentCount >= len(messages) {
		return -1, nil, nil
	}
	splitAt := len(messages) - recentCount
	return splitAt, messages[:splitAt], messages[splitAt:]
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
