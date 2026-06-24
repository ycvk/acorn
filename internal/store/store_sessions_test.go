package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestBindUserMessageRunIDByID(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, "sess_bind", "t"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Two unbound user messages on the same thread — the concurrency hazard that
	// the latest-unbound selector would mis-bind.
	m1, err := store.AppendSessionMessage(ctx, "sess_bind", 1, "user", "first", "")
	if err != nil {
		t.Fatalf("append m1: %v", err)
	}
	m2, err := store.AppendSessionMessage(ctx, "sess_bind", 2, "user", "second", "")
	if err != nil {
		t.Fatalf("append m2: %v", err)
	}

	// Binding by exact id binds that message, not the latest.
	if err := store.BindUserMessageRunIDByID(ctx, m1.ID, "run_a"); err != nil {
		t.Fatalf("bind m1: %v", err)
	}
	// m1 is bound; the latest unbound is still m2 (m1 was not skipped for latest,
	// and m2 was untouched).
	latest, err := store.LoadLatestUnboundUserMessage(ctx, "sess_bind")
	if err != nil {
		t.Fatalf("load latest unbound: %v", err)
	}
	if latest.ID != m2.ID {
		t.Fatalf("latest unbound id = %d, want m2 %d", latest.ID, m2.ID)
	}

	if err := store.BindUserMessageRunIDByID(ctx, m2.ID, "run_b"); err != nil {
		t.Fatalf("bind m2: %v", err)
	}
	if _, err := store.LoadLatestUnboundUserMessage(ctx, "sess_bind"); !errors.Is(err, ErrSessionMessageNotFound) {
		t.Fatalf("after binding both, want ErrSessionMessageNotFound, got %v", err)
	}

	// Re-binding an already-bound message fails loud (RowsAffected = 0), so a
	// concurrent create cannot silently steal an already-bound message.
	if err := store.BindUserMessageRunIDByID(ctx, m1.ID, "run_c"); err == nil {
		t.Fatal("re-binding an already-bound message should fail")
	}
	if err := store.BindUserMessageRunIDByID(ctx, 999999, "run_x"); err == nil {
		t.Fatal("binding a nonexistent message should fail")
	}
}

func TestSessionQueries(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	s1, err := store.CreateSession(context.Background(), "session_1", "first")
	if err != nil {
		t.Fatalf("create session 1: %v", err)
	}
	if _, err := store.AppendSessionMessage(context.Background(), s1.SessionID, 1, "user", "hello", ""); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_1", SessionID: s1.SessionID, TurnIndex: 1, Input: "hello"}); err != nil {
		t.Fatalf("create session run: %v", err)
	}
	if err := store.FinishRun(context.Background(), "run_1", core.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish session run: %v", err)
	}

	s2, err := store.CreateSession(context.Background(), "session_2", "second")
	if err != nil {
		t.Fatalf("create session 2: %v", err)
	}
	if _, err := store.AppendSessionMessage(context.Background(), s2.SessionID, 1, "user", "later", ""); err != nil {
		t.Fatalf("append session 2 message: %v", err)
	}

	sessions, err := store.ListSessions(context.Background(), 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != s2.SessionID {
		t.Fatalf("expected most recently updated session first, got %#v", sessions)
	}

	run, err := store.LoadLatestRunForSession(context.Background(), s1.SessionID)
	if err != nil {
		t.Fatalf("load latest run: %v", err)
	}
	if run == nil || run.RunID != "run_1" {
		t.Fatalf("unexpected latest run: %#v", run)
	}

	emptyRun, err := store.LoadLatestRunForSession(context.Background(), s2.SessionID)
	if err != nil {
		t.Fatalf("load latest run for empty session: %v", err)
	}
	if emptyRun != nil {
		t.Fatalf("expected nil latest run for empty session, got %#v", emptyRun)
	}
}

func TestLoadLatestRunsForSessionsReturnsNewestRunPerSession(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	alpha, err := store.CreateSession(context.Background(), "session_alpha", "alpha")
	if err != nil {
		t.Fatalf("create alpha session: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_alpha_1", SessionID: alpha.SessionID, TurnIndex: 1, Input: "first"}); err != nil {
		t.Fatalf("create alpha run 1: %v", err)
	}
	if err := store.FinishRun(context.Background(), "run_alpha_1", core.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish alpha run 1: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_alpha_2", SessionID: alpha.SessionID, TurnIndex: 2, Input: "second"}); err != nil {
		t.Fatalf("create alpha run 2: %v", err)
	}
	if err := store.FinishRun(context.Background(), "run_alpha_2", core.RunStatusFailed, "partial", "command failed"); err != nil {
		t.Fatalf("finish alpha run 2: %v", err)
	}

	beta, err := store.CreateSession(context.Background(), "session_beta", "beta")
	if err != nil {
		t.Fatalf("create beta session: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_beta_1", SessionID: beta.SessionID, TurnIndex: 1, Input: "approval"}); err != nil {
		t.Fatalf("create beta run: %v", err)
	}
	if err := store.MarkInterrupted(context.Background(), "run_beta_1", "waiting"); err != nil {
		t.Fatalf("mark beta interrupted: %v", err)
	}

	gamma, err := store.CreateSession(context.Background(), "session_gamma", "gamma")
	if err != nil {
		t.Fatalf("create gamma session: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_gamma_1", SessionID: gamma.SessionID, TurnIndex: 1, Input: "still going"}); err != nil {
		t.Fatalf("create gamma run: %v", err)
	}

	delta, err := store.CreateSession(context.Background(), "session_delta", "delta")
	if err != nil {
		t.Fatalf("create delta session: %v", err)
	}

	runs, err := store.LoadLatestRunsForSessions(context.Background(), []string{
		delta.SessionID,
		alpha.SessionID,
		beta.SessionID,
		gamma.SessionID,
		alpha.SessionID,
		"",
	})
	if err != nil {
		t.Fatalf("LoadLatestRunsForSessions: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected 3 latest runs, got %d", len(runs))
	}
	if _, ok := runs[delta.SessionID]; ok {
		t.Fatalf("expected session without runs to be absent, got %#v", runs[delta.SessionID])
	}

	alphaRun := runs[alpha.SessionID]
	if alphaRun == nil {
		t.Fatalf("missing alpha latest run")
	}
	if alphaRun.RunID != "run_alpha_2" || alphaRun.TurnIndex != 2 || alphaRun.Status != core.RunStatusFailed {
		t.Fatalf("unexpected alpha latest run: %#v", alphaRun)
	}

	betaRun := runs[beta.SessionID]
	if betaRun == nil {
		t.Fatalf("missing beta latest run")
	}
	if betaRun.RunID != "run_beta_1" || betaRun.Status != core.RunStatusInterrupted {
		t.Fatalf("unexpected beta latest run: %#v", betaRun)
	}

	gammaRun := runs[gamma.SessionID]
	if gammaRun == nil {
		t.Fatalf("missing gamma latest run")
	}
	if gammaRun.RunID != "run_gamma_1" || gammaRun.Status != core.RunStatusRunning {
		t.Fatalf("unexpected gamma latest run: %#v", gammaRun)
	}
}

func TestBindLatestUserMessageRunIDAndSyncAssistantMessageForRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_1", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.AppendSessionMessage(context.Background(), session.SessionID, 1, "user", "hello", ""); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_1", SessionID: session.SessionID, TurnIndex: 1, Input: "hello"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.BindLatestUserMessageRunID(context.Background(), session.SessionID, 1, "run_1"); err != nil {
		t.Fatalf("bind latest user message run id: %v", err)
	}
	if err := store.FinishRun(context.Background(), "run_1", core.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if _, err := store.AppendEvent(context.Background(), "run_1", "runtime.message", map[string]any{
		"message": map[string]any{
			"role":      "assistant",
			"content":   "done",
			"reasoning": "checked the repository state before answering",
		},
	}); err != nil {
		t.Fatalf("append agent message event: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_1"); err != nil {
		t.Fatalf("sync assistant message: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_1"); err != nil {
		t.Fatalf("sync assistant message second time: %v", err)
	}

	items, err := store.ListSessionMessages(context.Background(), session.SessionID, 10)
	if err != nil {
		t.Fatalf("list session messages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 session messages, got %d", len(items))
	}
	if items[0].RunID != "run_1" || items[0].Role != "user" {
		t.Fatalf("unexpected bound user message: %#v", items[0])
	}
	if items[1].RunID != "run_1" || items[1].Role != "assistant" || items[1].Content != "done" {
		t.Fatalf("unexpected synced assistant message: %#v", items[1])
	}
	if len(items[1].ContentParts) != 0 {
		t.Fatalf("assistant message should have no parts, got %#v", items[1].ContentParts)
	}
}

func TestClientSessionMessageHelpers(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_v1", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.UpdateSessionTitle(context.Background(), session.SessionID, "Client V1"); err != nil {
		t.Fatalf("UpdateSessionTitle: %v", err)
	}
	updated, err := store.LoadSession(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if updated.Title != "Client V1" {
		t.Fatalf("Title = %q, want Client V1", updated.Title)
	}

	turnIndex, err := store.NextSessionMessageTurnIndex(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("NextSessionMessageTurnIndex: %v", err)
	}
	if turnIndex != 1 {
		t.Fatalf("first turn index = %d, want 1", turnIndex)
	}
	first, err := store.AppendSessionMessage(context.Background(), session.SessionID, turnIndex, "user", "hello", "")
	if err != nil {
		t.Fatalf("AppendSessionMessage first: %v", err)
	}
	next, err := store.NextSessionMessageTurnIndex(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("NextSessionMessageTurnIndex after first: %v", err)
	}
	if next != 2 {
		t.Fatalf("next turn index = %d, want 2", next)
	}
	second, err := store.AppendSessionMessage(context.Background(), session.SessionID, next, "user", "run this", "")
	if err != nil {
		t.Fatalf("AppendSessionMessage second: %v", err)
	}
	latest, err := store.LoadLatestUnboundUserMessage(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("LoadLatestUnboundUserMessage: %v", err)
	}
	if latest.ID != second.ID || latest.Content != "run this" {
		t.Fatalf("latest = %#v, want second %#v", latest, second)
	}
	if err := store.BindLatestUserMessageRunID(context.Background(), session.SessionID, second.TurnIndex, "run_v1"); err != nil {
		t.Fatalf("BindLatestUserMessageRunID: %v", err)
	}
	latest, err = store.LoadLatestUnboundUserMessage(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("LoadLatestUnboundUserMessage after binding second: %v", err)
	}
	if latest.ID != first.ID {
		t.Fatalf("latest ID = %d, want first ID %d", latest.ID, first.ID)
	}
	if err := store.BindLatestUserMessageRunID(context.Background(), session.SessionID, first.TurnIndex, "run_v1_first"); err != nil {
		t.Fatalf("BindLatestUserMessageRunID first: %v", err)
	}
	_, err = store.LoadLatestUnboundUserMessage(context.Background(), session.SessionID)
	if !errors.Is(err, ErrSessionMessageNotFound) {
		t.Fatalf("LoadLatestUnboundUserMessage exhausted error = %v, want ErrSessionMessageNotFound", err)
	}
}

func TestUpdateSessionTitleIfEmptyTreatsWhitespaceAsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	session, err := store.CreateSession(ctx, "session_whitespace_title", "   ")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.UpdateSessionTitleIfEmpty(ctx, session.SessionID, "Generated title"); err != nil {
		t.Fatalf("UpdateSessionTitleIfEmpty: %v", err)
	}
	updated, err := store.LoadSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if updated.Title != "Generated title" {
		t.Fatalf("Title = %q, want Generated title", updated.Title)
	}

	if err := store.UpdateSessionTitleIfEmpty(ctx, session.SessionID, "Replacement title"); err != nil {
		t.Fatalf("UpdateSessionTitleIfEmpty replacement: %v", err)
	}
	unchanged, err := store.LoadSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("LoadSession unchanged: %v", err)
	}
	if unchanged.Title != "Generated title" {
		t.Fatalf("Title = %q, want Generated title", unchanged.Title)
	}
}

func TestSyncAssistantMessageForRunPersistsFailureContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_failure", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turnIndex, _, err := store.PrepareChatTurn(context.Background(), session.SessionID, "inspect repo", "inspect repo", 12)
	if err != nil {
		t.Fatalf("prepare chat turn: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_failed", SessionID: session.SessionID, TurnIndex: turnIndex, Input: "inspect repo"}); err != nil {
		t.Fatalf("create bound run: %v", err)
	}
	if err := store.FinishRun(context.Background(), "run_failed", core.RunStatusFailed, "rg stdout", "shell exited with status 1"); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_failed"); err != nil {
		t.Fatalf("sync assistant message: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_failed"); err != nil {
		t.Fatalf("sync assistant message second time: %v", err)
	}

	items, err := store.ListSessionMessages(context.Background(), session.SessionID, 12)
	if err != nil {
		t.Fatalf("list session messages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 session messages, got %d", len(items))
	}
	assistant := items[1]
	if assistant.RunID != "run_failed" || assistant.Role != "assistant" {
		t.Fatalf("unexpected assistant message: %#v", assistant)
	}
	if assistant.Content != "rg stdout" {
		t.Fatalf("assistant content = %q, want run output", assistant.Content)
	}
	if len(assistant.ContentParts) != 0 {
		t.Fatalf("assistant message should have no parts, got %#v", assistant.ContentParts)
	}
}

func TestSyncAssistantMessageForRunAllowsFinalSuccessAfterInterruptedContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_resume", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turnIndex, _, err := store.PrepareChatTurn(context.Background(), session.SessionID, "fix the command", "fix the command", 12)
	if err != nil {
		t.Fatalf("prepare chat turn: %v", err)
	}
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_resume", SessionID: session.SessionID, TurnIndex: turnIndex, Input: "fix the command"}); err != nil {
		t.Fatalf("create bound run: %v", err)
	}
	if err := store.MarkInterrupted(context.Background(), "run_resume", "partial output"); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_resume"); err != nil {
		t.Fatalf("sync interrupted message: %v", err)
	}
	if err := store.FinishRun(context.Background(), "run_resume", core.RunStatusSucceeded, "final answer", ""); err != nil {
		t.Fatalf("finish resumed run: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_resume"); err != nil {
		t.Fatalf("sync final message: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_resume"); err != nil {
		t.Fatalf("sync final message second time: %v", err)
	}

	items, err := store.ListSessionMessages(context.Background(), session.SessionID, 12)
	if err != nil {
		t.Fatalf("list session messages: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 session messages, got %d", len(items))
	}
	if items[1].Content != "partial output" {
		t.Fatalf("unexpected interrupted content: %q", items[1].Content)
	}
	if items[2].Content != "final answer" {
		t.Fatalf("unexpected final assistant content: %#v", items[2])
	}
	if len(items[1].ContentParts) != 0 || len(items[2].ContentParts) != 0 {
		t.Fatalf("assistant messages should have no parts")
	}
}

func TestPrepareChatTurnAndCreateBoundRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_1", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	turnIndex, items, err := store.PrepareChatTurn(context.Background(), session.SessionID, "hello acorn", "hello acorn", 12)
	if err != nil {
		t.Fatalf("prepare chat turn: %v", err)
	}
	if turnIndex != 1 {
		t.Fatalf("turnIndex = %d, want 1", turnIndex)
	}
	if len(items) != 1 || items[0].Role != "user" || items[0].Content != "hello acorn" {
		t.Fatalf("unexpected prepared items: %#v", items)
	}

	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_1", SessionID: session.SessionID, TurnIndex: turnIndex, Input: "hello acorn"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.BindLatestUserMessageRunID(context.Background(), session.SessionID, turnIndex, "run_1"); err != nil {
		t.Fatalf("bind latest user message: %v", err)
	}

	items, err = store.ListSessionMessages(context.Background(), session.SessionID, 12)
	if err != nil {
		t.Fatalf("list session messages: %v", err)
	}
	if len(items) != 1 || items[0].RunID != "run_1" {
		t.Fatalf("expected bound user message run_id, got %#v", items)
	}
}

func TestCreateFreshSessionTurn(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	turnIndex, err := store.CreateFreshSessionTurn(context.Background(), "session_fresh", "fresh child", "delegate this task")
	if err != nil {
		t.Fatalf("CreateFreshSessionTurn: %v", err)
	}
	if turnIndex != 1 {
		t.Fatalf("turnIndex = %d, want 1", turnIndex)
	}

	session, err := store.LoadSession(context.Background(), "session_fresh")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session.Title != "fresh child" {
		t.Fatalf("session title = %q, want %q", session.Title, "fresh child")
	}

	items, err := store.ListSessionMessages(context.Background(), "session_fresh", 10)
	if err != nil {
		t.Fatalf("ListSessionMessages: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("message count = %d, want 1", len(items))
	}
	if items[0].Content != "delegate this task" {
		t.Fatalf("message content = %q", items[0].Content)
	}
}

func TestCreateRunAndExplicitBindFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_orphan", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// CreateRun succeeds even without a user message to bind — binding is
	// a separate explicit step.
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_orphan", SessionID: session.SessionID, TurnIndex: 1, Input: "hello acorn"}); err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Explicit binding fails because no user message exists for this session.
	err = store.BindLatestUserMessageRunID(context.Background(), session.SessionID, 1, "run_orphan")
	if err == nil {
		t.Fatal("expected bind failure")
	}
	if !strings.Contains(err.Error(), "latest user session message not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The run still exists because CreateRun does not auto-cleanup on bind
	// failure — the caller is responsible for cleanup if needed.
	run, loadErr := store.LoadRun(context.Background(), "run_orphan")
	if loadErr != nil {
		t.Fatalf("LoadRun error = %v, expected run to exist", loadErr)
	}
	if run == nil {
		t.Fatal("expected run to exist after bind failure")
	}
}
