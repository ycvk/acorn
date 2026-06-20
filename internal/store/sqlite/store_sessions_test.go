package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	storecore "github.com/ycvk/acorn/internal/store"
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
	if _, err := store.LoadLatestUnboundUserMessage(ctx, "sess_bind"); !errors.Is(err, storecore.ErrSessionMessageNotFound) {
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
	if err := store.CreateRunWithSession(context.Background(), "run_1", s1.SessionID, 1, "hello", "run_1"); err != nil {
		t.Fatalf("create session run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_1", events.RunStatusSucceeded, "done", ""); err != nil {
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
	if err := store.CreateRunWithSession(context.Background(), "run_alpha_1", alpha.SessionID, 1, "first", "run_alpha_1"); err != nil {
		t.Fatalf("create alpha run 1: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_alpha_1", events.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish alpha run 1: %v", err)
	}
	if err := store.CreateRunWithSession(context.Background(), "run_alpha_2", alpha.SessionID, 2, "second", "run_alpha_2"); err != nil {
		t.Fatalf("create alpha run 2: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_alpha_2", events.RunStatusFailed, "partial", "command failed"); err != nil {
		t.Fatalf("finish alpha run 2: %v", err)
	}

	beta, err := store.CreateSession(context.Background(), "session_beta", "beta")
	if err != nil {
		t.Fatalf("create beta session: %v", err)
	}
	if err := store.CreateRunWithSession(context.Background(), "run_beta_1", beta.SessionID, 1, "approval", "run_beta_1"); err != nil {
		t.Fatalf("create beta run: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), "run_beta_1", "waiting"); err != nil {
		t.Fatalf("mark beta interrupted: %v", err)
	}

	gamma, err := store.CreateSession(context.Background(), "session_gamma", "gamma")
	if err != nil {
		t.Fatalf("create gamma session: %v", err)
	}
	if err := store.CreateRunWithSession(context.Background(), "run_gamma_1", gamma.SessionID, 1, "still going", "run_gamma_1"); err != nil {
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
	if alphaRun.RunID != "run_alpha_2" || alphaRun.TurnIndex != 2 || alphaRun.Status != events.RunStatusFailed {
		t.Fatalf("unexpected alpha latest run: %#v", alphaRun)
	}

	betaRun := runs[beta.SessionID]
	if betaRun == nil {
		t.Fatalf("missing beta latest run")
	}
	if betaRun.RunID != "run_beta_1" || betaRun.Status != events.RunStatusInterrupted {
		t.Fatalf("unexpected beta latest run: %#v", betaRun)
	}

	gammaRun := runs[gamma.SessionID]
	if gammaRun == nil {
		t.Fatalf("missing gamma latest run")
	}
	if gammaRun.RunID != "run_gamma_1" || gammaRun.Status != events.RunStatusRunning {
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
	if err := store.CreateRunWithSession(context.Background(), "run_1", session.SessionID, 1, "hello", "run_1"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.BindLatestUserMessageRunID(context.Background(), session.SessionID, 1, "run_1"); err != nil {
		t.Fatalf("bind latest user message run id: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_1", events.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_1", "agent.message", map[string]any{
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
	var parts []SessionMessagePart
	if err := json.Unmarshal(items[1].ContentParts, &parts); err != nil {
		t.Fatalf("unmarshal assistant message parts: %v", err)
	}
	if len(parts) != 4 {
		t.Fatalf("assistant parts length = %d, want 4: %#v", len(parts), parts)
	}
	if parts[0].Kind != "text" || parts[0].Text != "done" {
		t.Fatalf("assistant text part = %#v", parts[0])
	}
	if parts[1].Kind != "reasoning" || parts[1].Reasoning != "checked the repository state before answering" {
		t.Fatalf("assistant reasoning part = %#v", parts[1])
	}
	if parts[2].Kind != "result" || parts[2].Title != "Task completed" || len(parts[2].Changed) != 0 || len(parts[2].Verified) != 0 || len(parts[2].Risks) != 0 {
		t.Fatalf("assistant result part = %#v", parts[2])
	}
	if parts[3].Kind != "technical_detail_link" || parts[3].RunID != "run_1" || parts[3].DetailRunID != "run_1" {
		t.Fatalf("assistant technical detail part = %#v", parts[3])
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
	if !errors.Is(err, storecore.ErrSessionMessageNotFound) {
		t.Fatalf("LoadLatestUnboundUserMessage exhausted error = %v, want storecore.ErrSessionMessageNotFound", err)
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
	if err := store.CreateBoundRun(context.Background(), "run_failed", session.SessionID, turnIndex, "inspect repo", "run_failed"); err != nil {
		t.Fatalf("create bound run: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_failed", events.RunStatusFailed, "rg stdout", "shell exited with status 1"); err != nil {
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
	for _, want := range []string{
		"Acorn could not finish this turn.",
	} {
		if !strings.Contains(assistant.Content, want) {
			t.Fatalf("assistant failure context should contain %q, got %q", want, assistant.Content)
		}
	}
	for _, forbidden := range []string{"[run failed]", "run_failed", "last_output"} {
		if strings.Contains(assistant.Content, forbidden) {
			t.Fatalf("assistant failure content should not expose %q, got %q", forbidden, assistant.Content)
		}
	}
	var parts []SessionMessagePart
	if err := json.Unmarshal(assistant.ContentParts, &parts); err != nil {
		t.Fatalf("unmarshal failure parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("failure parts length = %d, want 2: %#v", len(parts), parts)
	}
	if parts[0].Kind != "work_status" || parts[0].Status != "failed" || parts[0].Title != "Acorn could not finish" {
		t.Fatalf("failure status part = %#v", parts[0])
	}
	if parts[0].Summary != "shell exited with status 1" || parts[0].Action != nil {
		t.Fatalf("failure status summary/action = %#v", parts[0])
	}
	if parts[1].Kind != "technical_detail_link" || parts[1].RunID != "run_failed" {
		t.Fatalf("failure technical detail part = %#v", parts[1])
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
	if err := store.CreateBoundRun(context.Background(), "run_resume", session.SessionID, turnIndex, "fix the command", "run_resume"); err != nil {
		t.Fatalf("create bound run: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), "run_resume", "partial output"); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}
	if err := store.SyncAssistantMessageForRun(context.Background(), "run_resume"); err != nil {
		t.Fatalf("sync interrupted message: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_resume", events.RunStatusSucceeded, "final answer", ""); err != nil {
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
	if !strings.Contains(items[1].Content, "Acorn paused before continuing.") {
		t.Fatalf("unexpected interrupted context: %q", items[1].Content)
	}
	if strings.Contains(items[1].Content, "[run interrupted]") || strings.Contains(items[1].Content, "run_resume") {
		t.Fatalf("interrupted content should not expose raw run id: %q", items[1].Content)
	}
	if items[2].Content != "final answer" {
		t.Fatalf("unexpected final assistant content: %#v", items[2])
	}
	var interruptedParts []SessionMessagePart
	if err := json.Unmarshal(items[1].ContentParts, &interruptedParts); err != nil {
		t.Fatalf("unmarshal interrupted parts: %v", err)
	}
	if len(interruptedParts) != 2 || interruptedParts[0].Kind != "work_status" || interruptedParts[0].Status != "interrupted" {
		t.Fatalf("interrupted parts = %#v", interruptedParts)
	}
	if interruptedParts[0].Action == nil || interruptedParts[0].Action.Kind != "resume_run" || interruptedParts[0].Action.RunID != "run_resume" {
		t.Fatalf("interrupted action = %#v", interruptedParts[0].Action)
	}
	var finalParts []SessionMessagePart
	if err := json.Unmarshal(items[2].ContentParts, &finalParts); err != nil {
		t.Fatalf("unmarshal final parts: %v", err)
	}
	if len(finalParts) != 3 || finalParts[1].Kind != "result" || finalParts[2].Kind != "technical_detail_link" {
		t.Fatalf("final parts = %#v", finalParts)
	}
}

func TestSyncDecisionMessageForPendingActionCreatesAndUpdatesDecisionPart(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), "session_decision", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turnIndex, _, err := store.PrepareChatTurn(context.Background(), session.SessionID, "run tool", "run tool", 12)
	if err != nil {
		t.Fatalf("prepare chat turn: %v", err)
	}
	if err := store.CreateBoundRun(context.Background(), "run_decision", session.SessionID, turnIndex, "run tool", "run_decision"); err != nil {
		t.Fatalf("create bound run: %v", err)
	}
	_, err = store.CreatePendingAction(context.Background(), storecore.CreatePendingActionInput{
		ActionID:    "action_decision",
		RunID:       "run_decision",
		Kind:        events.PendingActionKindElicitation,
		Subject:     "elicitation",
		PayloadJSON: `{"message":"Allow Acorn to continue?"}`,
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
	})
	if err != nil {
		t.Fatalf("create pending action: %v", err)
	}
	if err := store.SyncDecisionMessageForPendingAction(context.Background(), "action_decision"); err != nil {
		t.Fatalf("sync pending decision message: %v", err)
	}

	items, err := store.ListSessionMessages(context.Background(), session.SessionID, 12)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("messages len = %d, want 2", len(items))
	}
	var parts []SessionMessagePart
	if err := json.Unmarshal(items[1].ContentParts, &parts); err != nil {
		t.Fatalf("unmarshal pending parts: %v", err)
	}
	if len(parts) != 2 || parts[0].Kind != "decision" {
		t.Fatalf("pending decision parts = %#v", parts)
	}
	if parts[0].DecisionID != "action_decision" || parts[0].Status != "pending" || parts[0].SelectedOptionID != "" {
		t.Fatalf("pending decision part = %#v", parts[0])
	}
	if parts[0].Options[0].ID != "accept" || parts[0].Options[1].ID != "decline" {
		t.Fatalf("pending options = %#v", parts[0].Options)
	}

	if _, err := store.DecidePendingAction(context.Background(), "action_decision", events.PendingActionStatusApproved, events.PendingActionModeDeferred, `{"action":"accept"}`); err != nil {
		t.Fatalf("decide pending action: %v", err)
	}
	if err := store.SyncDecisionMessageForPendingAction(context.Background(), "action_decision"); err != nil {
		t.Fatalf("sync decided decision message: %v", err)
	}
	items, err = store.ListSessionMessages(context.Background(), session.SessionID, 12)
	if err != nil {
		t.Fatalf("list messages after decide: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("messages len after decide = %d, want 2", len(items))
	}
	parts = nil
	if err := json.Unmarshal(items[1].ContentParts, &parts); err != nil {
		t.Fatalf("unmarshal decided parts: %v", err)
	}
	if parts[0].Status != "approved" || parts[0].SelectedOptionID != "accept" {
		t.Fatalf("decided decision part = %#v", parts[0])
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

	if err := store.CreateBoundRun(context.Background(), "run_1", session.SessionID, turnIndex, "hello acorn", "run_1"); err != nil {
		t.Fatalf("create bound run: %v", err)
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

func TestCreateBoundRunCleansUpRunWhenBindingFails(t *testing.T) {
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

	err = store.CreateBoundRun(context.Background(), "run_orphan", session.SessionID, 1, "hello acorn", "run_orphan")
	if err == nil {
		t.Fatal("expected bind failure")
	}
	if !strings.Contains(err.Error(), "latest user session message not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	run, loadErr := store.LoadRun(context.Background(), "run_orphan")
	if !errors.Is(loadErr, storecore.ErrRunNotFound) {
		t.Fatalf("LoadRun error = %v, want storecore.ErrRunNotFound", loadErr)
	}
	if run != nil {
		t.Fatalf("expected orphaned run cleanup, got %#v", run)
	}
}
