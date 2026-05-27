package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

func TestChatServiceSendPersistsFailureContextForFollowUp(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, err := store.CreateSession(ctx, "session_1", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	service := NewChatService(store, func(_ context.Context) (executorHandle, error) {
		return chatTestExecutorHandle{
			executeMessagesFn: func(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*runtime.Result, error) {
				if err := store.CreateBoundRun(context.Background(), "run_failed", req.SessionID, req.TurnIndex, req.Input, "run_failed"); err != nil {
					t.Fatalf("create bound run: %v", err)
				}
				if err := store.FinishRunContext(context.Background(), "run_failed", events.RunStatusFailed, "partial shell output", "shell exited with status 1"); err != nil {
					t.Fatalf("finish run: %v", err)
				}
				return &runtime.Result{
					RunID:  "run_failed",
					Status: events.RunStatusFailed,
					Output: "partial shell output",
					Error:  "shell exited with status 1",
				}, nil
			},
		}, nil
	})

	result, turnIndex, err := service.Send(ctx, session.SessionID, "fix the broken command", "", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if turnIndex != 1 {
		t.Fatalf("turnIndex = %d, want 1", turnIndex)
	}
	if result == nil || result.Status != events.RunStatusFailed {
		t.Fatalf("unexpected result: %#v", result)
	}

	items, err := store.ListSessionMessages(ctx, session.SessionID, 12)
	if err != nil {
		t.Fatalf("list session messages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 session messages, got %#v", items)
	}
	if items[1].Role != "assistant" || items[1].RunID != "run_failed" {
		t.Fatalf("unexpected synced assistant message: %#v", items[1])
	}
	if !strings.Contains(items[1].Content, "Acorn could not finish this turn.") {
		t.Fatalf("assistant failure context missing terminal summary: %q", items[1].Content)
	}
	for _, forbidden := range []string{"[run failed]", "run_failed", "Continue debugging with chat or trace instead of resume."} {
		if strings.Contains(items[1].Content, forbidden) {
			t.Fatalf("assistant failure context should not contain %q: %q", forbidden, items[1].Content)
		}
	}
}

func TestChatServiceSendPassesInputThroughExecuteRequest(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, err := store.CreateSession(ctx, "session_1", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	var captured runtimeapi.ExecuteRequest
	service := NewChatService(store, func(_ context.Context) (executorHandle, error) {
		return chatTestExecutorHandle{
			executeMessagesFn: func(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*runtime.Result, error) {
				captured = req
				if err := store.CreateBoundRun(context.Background(), "run_success", req.SessionID, req.TurnIndex, req.Input, "run_success"); err != nil {
					t.Fatalf("create bound run: %v", err)
				}
				if err := store.FinishRunContext(context.Background(), "run_success", events.RunStatusSucceeded, "done", ""); err != nil {
					t.Fatalf("finish run: %v", err)
				}
				return &runtime.Result{
					RunID:  "run_success",
					Status: events.RunStatusSucceeded,
					Output: "done",
				}, nil
			},
		}, nil
	})

	result, turnIndex, err := service.Send(ctx, session.SessionID, "remember where the repo root is", "skill.inspect.repo", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if turnIndex != 1 {
		t.Fatalf("turnIndex = %d, want 1", turnIndex)
	}
	if result == nil || result.Status != events.RunStatusSucceeded {
		t.Fatalf("unexpected result: %#v", result)
	}
	if captured.Input != "remember where the repo root is" {
		t.Fatalf("captured input = %q, want original prompt", captured.Input)
	}
	if captured.SessionID != session.SessionID {
		t.Fatalf("captured session_id = %q, want %q", captured.SessionID, session.SessionID)
	}
	if captured.SkillID != "skill.inspect.repo" {
		t.Fatalf("captured skill_id = %q, want skill.inspect.repo", captured.SkillID)
	}
	if len(captured.Messages) != 1 {
		t.Fatalf("len(captured.Messages) = %d, want 1", len(captured.Messages))
	}
}

type chatTestExecutorHandle struct {
	executeMessagesFn func(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*runtime.Result, error)
}

func (h chatTestExecutorHandle) Run(ctx context.Context, input, skillID string, sink stream.StreamSink) (*runtime.Result, error) {
	panic("unexpected Run call")
}

func (h chatTestExecutorHandle) ExecuteMessages(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*runtime.Result, error) {
	if h.executeMessagesFn == nil {
		panic("unexpected ExecuteMessages call")
	}
	return h.executeMessagesFn(ctx, req, sink)
}

func (h chatTestExecutorHandle) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink stream.StreamSink) (*runtime.Result, error) {
	panic("unexpected ResumeWithTargets call")
}
