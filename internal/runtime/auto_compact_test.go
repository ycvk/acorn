package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestAutoCompactorSplitPointPreservesRecentTurns(t *testing.T) {
	c := newAutoCompactor(&stubSummaryModel{}, testTokenCounter(t), 2) // keep last 2*2=4 messages
	conversation := []adk.Message{
		schema.UserMessage("old 1"),
		schema.AssistantMessage("resp 1", nil),
		schema.UserMessage("recent user"),
		schema.AssistantMessage("recent assistant", nil),
		schema.UserMessage("latest user"),
		schema.AssistantMessage("latest assistant", nil),
	}
	if got := c.splitPoint(conversation); got != 2 {
		t.Fatalf("splitPoint = %d, want 2", got)
	}
}

func TestAutoCompactorSplitPointNothingToCompact(t *testing.T) {
	c := newAutoCompactor(&stubSummaryModel{}, testTokenCounter(t), 10)
	if got := c.splitPoint([]adk.Message{schema.UserMessage("only message")}); got != -1 {
		t.Fatalf("splitPoint = %d, want -1", got)
	}
}

// The live zone must not start with a tool result: it would be orphaned from
// the assistant tool call it answers once the compact zone is summarized.
func TestAutoCompactorSplitPointDoesNotOrphanToolResults(t *testing.T) {
	c := newAutoCompactor(&stubSummaryModel{}, testTokenCounter(t), 1) // keep last 2 messages
	call := schema.ToolCall{ID: "call_1", Function: schema.FunctionCall{Name: "read_file"}}
	conversation := []adk.Message{
		schema.UserMessage("old"),
		schema.AssistantMessage("", []schema.ToolCall{call}),
		schema.ToolMessage("file a", "call_1"),
		schema.ToolMessage("file b", "call_1"),
		schema.AssistantMessage("done", nil),
	}
	// Naive split would be 3 (a tool message); it must advance past tool results.
	if got := c.splitPoint(conversation); got != 4 {
		t.Fatalf("splitPoint = %d, want 4", got)
	}
}

// Regression: the session used to prepend the summary to the full message list
// without dropping the summarized messages, so compaction grew the context
// instead of shrinking it. It must replace the compact zone and keep the
// bootstrap prefix (assembled context + system instruction) verbatim.
func TestContextSessionCompactionReplacesSummarizedMessagesAndKeepsPrefix(t *testing.T) {
	session := NewDefaultSession(SessionOptions{
		TokenCounter:        testTokenCounter(t),
		Model:               &stubSummaryModel{response: "the summary"},
		WindowTokens:        10,
		CompactMargin:       1,
		PreserveRecentTurns: 1,
	})
	ctx := context.Background()
	if _, err := session.Bootstrap(ctx, BootstrapRequest{
		SessionID: "session_1",
		RunID:     "run_1",
		InitialMessages: []adk.Message{
			schema.SystemMessage("instruction"),
			schema.UserMessage("old request"),
		},
		Assembly: &AssembleResult{Messages: []*schema.Message{schema.UserMessage("memory context")}},
	}); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := session.RecordMessages(ctx, []adk.Message{
		schema.AssistantMessage("old answer", nil),
		schema.UserMessage("recent request"),
		schema.AssistantMessage("recent answer", nil),
	}); err != nil {
		t.Fatalf("RecordMessages: %v", err)
	}

	// Over threshold: starts the background summary of the compact zone.
	if _, err := session.BeforeModelCall(ctx, ModelCallRequest{CallID: "call_1"}); err != nil {
		t.Fatalf("BeforeModelCall 1: %v", err)
	}
	waitForPendingCompact(t, session.(*defaultContextSession).compactor, 2*time.Second)
	if err := session.RecordMessages(ctx, []adk.Message{schema.UserMessage("new request")}); err != nil {
		t.Fatalf("RecordMessages: %v", err)
	}

	// Next turn splices the settled summary in place of the compact zone.
	input, err := session.BeforeModelCall(ctx, ModelCallRequest{CallID: "call_2"})
	if err != nil {
		t.Fatalf("BeforeModelCall 2: %v", err)
	}
	got := messageContents(input.Messages)
	want := []string{
		"memory context",
		"instruction",
		"Conversation summary:\nthe summary",
		"recent request",
		"recent answer",
		"new request",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("messages =\n%q\nwant\n%q", got, want)
	}
}

// --- helpers ---

// waitForPendingCompact waits until the in-flight background compaction has
// settled (completed or failed), or fails the test after deadline.
func waitForPendingCompact(t *testing.T, c *autoCompactor, deadline time.Duration) {
	t.Helper()
	c.mu.Lock()
	p := c.pending
	c.mu.Unlock()
	if p == nil {
		t.Fatal("no pending compaction to wait for")
	}
	select {
	case <-p.done:
	case <-time.After(deadline):
		t.Fatalf("pending compact did not complete within %v", deadline)
	}
}

type stubSummaryModel struct {
	response string
}

func (m *stubSummaryModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.response, nil), nil
}

func (m *stubSummaryModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, context.Canceled
}
