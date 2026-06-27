package runtime

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// stubDelayedSummaryModel blocks on Generate until release() is called, then
// returns the configured response. It lets tests verify that maybeStartCompact
// returns immediately while the summary is still being generated in the
// background.
type stubDelayedSummaryModel struct {
	response string
	done     chan struct{}
	entered  chan struct{}
	calls    atomic.Int32
}

func newStubDelayedSummaryModel(response string) *stubDelayedSummaryModel {
	return &stubDelayedSummaryModel{
		response: response,
		done:     make(chan struct{}),
		entered:  make(chan struct{}),
	}
}

func (m *stubDelayedSummaryModel) release() { close(m.done) }

// waitEntered blocks until the background goroutine has entered Generate, or
// fails the test after a timeout. This replaces polling calls.Load().
func (m *stubDelayedSummaryModel) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-m.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("stubDelayedSummaryModel.Generate was not entered within 2s")
	}
}

func (m *stubDelayedSummaryModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.calls.Add(1)
	close(m.entered)
	<-m.done
	return schema.AssistantMessage(m.response, nil), nil
}

func (m *stubDelayedSummaryModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, context.Canceled
}

// stubErrorModelWithCount is a stubErrorModel variant that tracks call count
// for circuit breaker verification.
type stubErrorModelWithCount struct {
	calls atomic.Int32
}

func (m *stubErrorModelWithCount) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.calls.Add(1)
	return nil, context.DeadlineExceeded
}

func (m *stubErrorModelWithCount) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, context.Canceled
}

// TestAutoCompactorMaybeStartCompactReturnsImmediately verifies that
// maybeStartCompact starts a background summary but returns the original
// messages immediately, without blocking on model.Generate.
func TestAutoCompactorMaybeStartCompactReturnsImmediately(t *testing.T) {
	counter := testTokenCounter(t)
	model := newStubDelayedSummaryModel("summary of conversation")
	c := newAutoCompactor(model, counter, 1)

	msgs := []adk.Message{
		schema.UserMessage("old request 1"),
		schema.AssistantMessage("old response 1", nil),
		schema.UserMessage("recent request"),
	}

	// Use a context with a short timeout to prove maybeStartCompact does not
	// block waiting for the model: if it did, the deadline would fire.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := c.maybeStartCompact(ctx, msgs)

	// The returned slice must be the original messages unchanged — the summary
	// is still being generated in the background.
	if len(result) != len(msgs) {
		t.Fatalf("maybeStartCompact returned %d messages, want %d (original unchanged)", len(result), len(msgs))
	}
	if result[0].Content != "old request 1" {
		t.Fatalf("first message = %q, want original unchanged", result[0].Content)
	}

	// Confirm the background Generate call was actually started.
	model.waitEntered(t)

	// Release the background model call so the goroutine can finish.
	model.release()

	// Wait for the pending summary to become available.
	waitForPendingCompact(t, c, 2*time.Second)
}

// TestAutoCompactorApplyPendingCompactSplicesSummary verifies that after a
// background summary completes, applyPendingCompact replaces the compact zone
// with [summary + recent messages] while keeping any messages appended to the
// live zone since compaction started.
func TestAutoCompactorApplyPendingCompactSplicesSummary(t *testing.T) {
	counter := testTokenCounter(t)
	model := newStubDelayedSummaryModel("the summary")
	c := newAutoCompactor(model, counter, 1)

	msgs := []adk.Message{
		schema.UserMessage("old request 1"),
		schema.AssistantMessage("old response 1", nil),
		schema.UserMessage("recent request"),
	}

	c.maybeStartCompact(context.Background(), msgs)
	model.release()
	waitForPendingCompact(t, c, 2*time.Second)

	// While compaction was in flight, two more messages arrived in the live
	// zone (a new turn).
	live := []adk.Message{
		schema.UserMessage("recent request"),
		schema.AssistantMessage("recent answer", nil),
		schema.UserMessage("new user turn"),
		schema.AssistantMessage("new assistant turn", nil),
	}

	result := c.applyPendingCompact(live)

	// Expected: [summary, recent request, recent answer, new user turn, new assistant turn]
	if len(result) != 5 {
		t.Fatalf("result length = %d, want 5", len(result))
	}
	if result[0].Role != schema.System || !strings.Contains(result[0].Content, "the summary") {
		t.Fatalf("first message = %+v, want system summary containing 'the summary'", result[0])
	}
	if result[1].Content != "recent request" {
		t.Fatalf("second message = %q, want 'recent request'", result[1].Content)
	}
	if result[4].Content != "new assistant turn" {
		t.Fatalf("last message = %q, want 'new assistant turn'", result[4].Content)
	}
}

// TestAutoCompactorApplyPendingCompactNoopWhenNotReady verifies that
// applyPendingCompact returns the input unchanged when no background
// compaction has been started or the pending one has not completed yet.
func TestAutoCompactorApplyPendingCompactNoopWhenNotReady(t *testing.T) {
	counter := testTokenCounter(t)
	model := newStubDelayedSummaryModel("summary")
	c := newAutoCompactor(model, counter, 1)

	live := []adk.Message{schema.UserMessage("only message")}

	// No compaction started → must return input unchanged.
	result := c.applyPendingCompact(live)
	if len(result) != 1 || result[0].Content != "only message" {
		t.Fatalf("applyPendingCompact without pending = %v, want unchanged", result)
	}

	// Compaction started but not released yet → must still return input unchanged.
	msgs := []adk.Message{
		schema.UserMessage("old request 1"),
		schema.AssistantMessage("old response 1", nil),
		schema.UserMessage("recent request"),
	}
	c.maybeStartCompact(context.Background(), msgs)
	model.waitEntered(t)
	result = c.applyPendingCompact(live)
	if len(result) != 1 || result[0].Content != "only message" {
		t.Fatalf("applyPendingCompact with incomplete pending = %v, want unchanged", result)
	}

	model.release()
}

// TestAutoCompactorCircuitBreakerWithNonBlocking verifies the circuit breaker
// still trips after maxFailures even with the non-blocking path.
func TestAutoCompactorCircuitBreakerWithNonBlocking(t *testing.T) {
	counter := testTokenCounter(t)
	model := &stubErrorModelWithCount{}
	c := newAutoCompactor(model, counter, 1)

	msgs := []adk.Message{
		schema.UserMessage("old 1"),
		schema.AssistantMessage("resp 1", nil),
		schema.UserMessage("recent"),
	}

	// Start 3 failing compactions; each completes immediately because the
	// error model returns synchronously.
	for i := 0; i < autoCompactMaxFailures; i++ {
		c.maybeStartCompact(context.Background(), msgs)
		waitForPendingCompact(t, c, 2*time.Second)
	}

	if c.failures != autoCompactMaxFailures {
		t.Fatalf("failures = %d, want %d", c.failures, autoCompactMaxFailures)
	}

	// Circuit breaker tripped: maybeStartCompact should be a no-op.
	c.maybeStartCompact(context.Background(), msgs)
	if model.calls.Load() != int32(autoCompactMaxFailures) {
		t.Fatalf("circuit breaker should not start new compaction, calls = %d", model.calls.Load())
	}
}

// waitForPendingCompact polls pendingCompactDone until it returns true or the
// deadline expires. The summary goroutine runs asynchronously, so the test
// must wait for it to settle.
func waitForPendingCompact(t *testing.T, c *autoCompactor, deadline time.Duration) {
	t.Helper()
	deadlineCh := time.After(deadline)
	for {
		select {
		case <-deadlineCh:
			t.Fatalf("pending compact did not complete within %v", deadline)
		default:
		}
		if c.pendingCompactDone() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}
