package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestInboxServiceLoadProjectsMobileAggregate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, "thread_active", "Active"); err != nil {
		t.Fatalf("create active session: %v", err)
	}
	if _, err := store.CreateSession(ctx, "thread_terminal", "Done"); err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	if err := store.CreateRunWithParams(ctx, storecore.RunCreateParams{
		RunID:             "run_active",
		SessionID:         "thread_active",
		TurnIndex:         1,
		Input:             "work",
		CheckpointID:      "run_active",
		OrchestrationMode: orchestrationmode.PlanExecute,
	}); err != nil {
		t.Fatalf("create active run: %v", err)
	}
	if err := store.CreateRunWithParams(ctx, storecore.RunCreateParams{
		RunID:             "run_terminal",
		SessionID:         "thread_terminal",
		TurnIndex:         1,
		Input:             "finish",
		CheckpointID:      "run_terminal",
		OrchestrationMode: orchestrationmode.DirectResponse,
	}); err != nil {
		t.Fatalf("create terminal run: %v", err)
	}
	if err := store.FinishRunContext(ctx, "run_terminal", events.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish terminal run: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_1",
		RunID:       "run_active",
		Kind:        events.PendingActionKindElicitation,
		Subject:     "Approval required",
		PayloadJSON: `{"message":"Allow Acorn to continue?"}`,
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}

	service := NewInboxService(store, &inboxCapabilityStub{snapshot: SystemCapabilities{
		RuntimeReadiness: &RuntimeReadiness{Status: RuntimeReadinessReady},
		Model:            SystemModelCapabilities{Name: "gpt-test"},
		Features:         SystemFeatureCapabilities{InterruptResume: true},
	}})
	inbox, err := service.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(inbox.PendingActions) != 1 {
		t.Fatalf("pending actions = %#v, want one", inbox.PendingActions)
	}
	action := inbox.PendingActions[0]
	if action.ActionID != "action_1" || action.ThreadID != "thread_active" || action.Body != "Allow Acorn to continue?" {
		t.Fatalf("pending action summary = %#v", action)
	}
	if len(action.Options) != 2 || action.Options[0].ID != "accept" || action.Options[1].ID != "decline" {
		t.Fatalf("pending options = %#v", action.Options)
	}
	if len(inbox.ActiveRuns) != 1 || inbox.ActiveRuns[0].RunID != "run_active" || inbox.ActiveRuns[0].Mode != "plan_execute" {
		t.Fatalf("active runs = %#v", inbox.ActiveRuns)
	}
	if len(inbox.RecentTerminalRuns) != 1 || inbox.RecentTerminalRuns[0].RunID != "run_terminal" || inbox.RecentTerminalRuns[0].Status != "completed" {
		t.Fatalf("terminal runs = %#v", inbox.RecentTerminalRuns)
	}
	if inbox.System.RuntimeReadiness == nil || inbox.System.RuntimeReadiness.Status != RuntimeReadinessReady {
		t.Fatalf("system snapshot = %#v", inbox.System)
	}
}

func TestInboxServiceFailsOnInvalidPendingActionPayload(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, "thread_invalid_payload", "Invalid"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_invalid_payload", "thread_invalid_payload", 1, "work", "run_invalid_payload"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_invalid_payload",
		RunID:       "run_invalid_payload",
		Kind:        events.PendingActionKindElicitation,
		PayloadJSON: `{"message":`,
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}

	_, err := NewInboxService(store, &inboxCapabilityStub{}).Load(ctx)
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("Load error = %v, want ErrClientProjectionFailed", err)
	}
}

func TestInboxServiceProjectsOperatorQuestionPendingAction(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, "thread_operator_pending", "Question"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_operator_pending", "thread_operator_pending", 1, "ask", "run_operator_pending"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_operator_pending",
		RunID:    "run_operator_pending",
		Kind:     events.PendingActionKindOperatorQuestion,
		Subject:  "Choose path",
		PayloadJSON: `{
			"question":"Which path should Acorn take?",
			"options":[{"id":"fast","label":"Fast","description":"Ship the focused change"}],
			"allow_freeform":true
		}`,
		Status: events.PendingActionStatusPending,
		Mode:   events.PendingActionModeDeferred,
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}

	inbox, err := NewInboxService(store, &inboxCapabilityStub{}).Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(inbox.PendingActions) != 1 {
		t.Fatalf("pending actions = %#v, want one", inbox.PendingActions)
	}
	action := inbox.PendingActions[0]
	if action.Kind != "operator_question" || action.Title != "Choose path" || action.Body != "Which path should Acorn take?" {
		t.Fatalf("operator summary = %#v", action)
	}
	if len(action.Options) != 1 || action.Options[0].ID != "fast" || action.Options[0].Description != "Ship the focused change" {
		t.Fatalf("operator options = %#v", action.Options)
	}
}

type inboxCapabilityStub struct {
	snapshot SystemCapabilities
}

func (s *inboxCapabilityStub) Snapshot(context.Context, CapabilitySnapshotOptions) SystemCapabilities {
	return s.snapshot
}
