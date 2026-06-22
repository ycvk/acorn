package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestContextSessionBootstrapOrdersAssemblyBeforeInitialMessages(t *testing.T) {
	session := newTestContextSession(t)
	input, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		InitialMessages: []adk.Message{schema.UserMessage("user request")},
		Assembly: &AssembleResult{Messages: []*schema.Message{
			schema.UserMessage("memory context"),
			schema.UserMessage("skill context"),
		}},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	got := messageContents(input.Messages)
	want := []string{"memory context", "skill context", "user request"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("messages = %v, want %v", got, want)
	}
	if session.ID().SessionID != "session_1" || session.ID().RunID != "run_1" {
		t.Fatalf("unexpected session id: %+v", session.ID())
	}
}

func TestContextSessionModelInputReturnsCopies(t *testing.T) {
	session := newTestContextSession(t)
	input, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		InitialMessages: []adk.Message{schema.UserMessage("original")},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	input.Messages[0].Content = "mutated outside"
	next, err := session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_1"})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	if got := next.Messages[0].Content; got != "original" {
		t.Fatalf("session message = %q, want original", got)
	}
}

func TestContextSessionRecordsAssistantAndToolResults(t *testing.T) {
	session := newTestContextSession(t)
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		InitialMessages: []adk.Message{schema.UserMessage("request")},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := session.RecordAssistant(context.Background(), schema.AssistantMessage("assistant", nil)); err != nil {
		t.Fatalf("RecordAssistant: %v", err)
	}
	if err := session.RecordToolResults(context.Background(), []adk.Message{schema.ToolMessage("result", "call_1")}); err != nil {
		t.Fatalf("RecordToolResults: %v", err)
	}
	input, err := session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_2"})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	got := messageContents(input.Messages)
	want := []string{"request", "assistant", "result"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("messages = %v, want %v", got, want)
	}
}

func TestContextSessionContextBinding(t *testing.T) {
	session := newTestContextSession(t)
	ctx := WithContextSession(context.Background(), session)
	if got := ContextSessionFromContext(ctx); got != session {
		t.Fatalf("ContextSessionFromContext = %v, want bound session", got)
	}
	if got := ContextSessionFromContext(context.Background()); got != nil {
		t.Fatalf("ContextSessionFromContext without binding = %v, want nil", got)
	}
}

func TestContextSessionBootstrapRejectsMissingIdentity(t *testing.T) {
	_, err := newTestContextSession(t).Bootstrap(context.Background(), BootstrapRequest{
		RunID: "run_1",
	})
	if err == nil || !strings.Contains(err.Error(), "context session id is required") {
		t.Fatalf("error = %v, want session id required", err)
	}
}

func TestContextSessionRequiresTokenCounter(t *testing.T) {
	_, err := NewDefaultContextSession(ContextSessionOptions{}).Bootstrap(context.Background(), BootstrapRequest{
		SessionID: "session_1",
		RunID:     "run_1",
	})
	if err == nil || !strings.Contains(err.Error(), "token counter is required") {
		t.Fatalf("error = %v, want token counter required", err)
	}
}

func TestContextSessionBeforeModelCallRequiresBootstrap(t *testing.T) {
	session := NewDefaultContextSession(ContextSessionOptions{
		TokenCounter: testTokenCounter(t),
	})
	_, err := session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_1"})
	if err == nil || !strings.Contains(err.Error(), "must be bootstrapped") {
		t.Fatalf("error = %v, want bootstrapped required", err)
	}
}

// --- helpers ---

func messageContents(messages []adk.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		out = append(out, msg.Content)
	}
	return out
}

func newTestContextSession(t *testing.T) ContextSession {
	t.Helper()
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter: %v", err)
	}
	return NewDefaultContextSession(ContextSessionOptions{
		TokenCounter:        counter,
		WindowTokens:        200000,
		CompactMargin:       13000,
		MaskAfterTurns:      2,
		PreserveRecentTurns: 3,
	})
}
