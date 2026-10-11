package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestLatestRepliesAndToolCallEvents(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, id := range []string{"session_a", "session_b", "session_empty"} {
		if _, err := store.CreateSession(ctx, id, id); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	if err := store.CreateRun(ctx, core.RunCreateParams{RunID: "run_a", SessionID: "session_a", TurnIndex: 1, Input: "find it"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, m := range []struct{ session, role, content, run string }{
		{"session_a", "user", "find it", "run_a"},
		{"session_a", "assistant", "found it", "run_a"},
		{"session_a", "wake", "[night 2026-10-11] night reflection", ""},
		{"session_b", "user", "only a question", ""},
		{"session_b", "assistant", "", ""},
	} {
		if _, err := store.AppendSessionMessage(ctx, m.session, 1, m.role, m.content, m.run); err != nil {
			t.Fatalf("append message: %v", err)
		}
	}

	replies, err := store.LoadLatestRepliesForSessions(ctx, []string{"session_a", "session_b", "session_empty", "session_a", " "})
	if err != nil {
		t.Fatalf("latest replies: %v", err)
	}
	if len(replies) != 2 || replies["session_a"].Content != "found it" || replies["session_a"].Role != "assistant" || replies["session_b"].Content != "only a question" {
		t.Fatalf("replies = %+v", replies)
	}

	for _, e := range []struct {
		kind    string
		payload map[string]any
	}{
		{"tool.call.started", map[string]any{"tool_call_id": "call_1", "tool_name": "web_search", "arguments": `{"query":"go 1.28"}`}},
		{"assistant.delta", map[string]any{"assistant_delta": map[string]any{"delta": "x"}}},
		{"tool.call.succeeded", map[string]any{"tool_call_id": "call_1", "tool_name": "web_search", "output": "results"}},
	} {
		if _, err := store.AppendEvent(ctx, "run_a", e.kind, e.payload); err != nil {
			t.Fatalf("append %s: %v", e.kind, err)
		}
	}
	events, err := store.LoadToolCallEvents(ctx, []string{"run_a", "run_missing"})
	if err != nil {
		t.Fatalf("tool call events: %v", err)
	}
	got := events["run_a"]
	if len(events) != 1 || len(got) != 2 || got[0].Kind != "tool.call.started" || got[1].Kind != "tool.call.succeeded" || got[0].RunID != "run_a" {
		t.Fatalf("events = %+v", events)
	}
}
