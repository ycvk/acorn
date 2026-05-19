package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	storecore "github.com/ycvk/acorn/internal/store"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func TestPendingActionDecisionStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision string
		want     events.PendingActionStatus
	}{
		{name: "accept", decision: "accept", want: events.PendingActionStatusApproved},
		{name: "decline", decision: "decline", want: events.PendingActionStatusRejected},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := pendingActionDecisionStatus(test.decision)
			if err != nil {
				t.Fatalf("pendingActionDecisionStatus: %v", err)
			}
			if got != test.want {
				t.Fatalf("pendingActionDecisionStatus = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPendingActionDecisionStatusRejectsUnsupportedDecision(t *testing.T) {
	t.Parallel()

	if _, err := pendingActionDecisionStatus("maybe"); !errors.Is(err, ErrPendingActionDecisionInvalid) {
		t.Fatalf("pendingActionDecisionStatus() error = %v, want %v", err, ErrPendingActionDecisionInvalid)
	}
}

func TestPendingActionServiceDecideSyncsMessageAndElicitationEvent(t *testing.T) {
	store := openTestStore(t)

	ctx := context.Background()
	session, err := store.CreateSession(ctx, "session_decision_service", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turnIndex, _, err := store.PrepareChatTurn(ctx, session.SessionID, "run tool", "run tool", 12)
	if err != nil {
		t.Fatalf("prepare chat turn: %v", err)
	}
	if err := store.CreateBoundRun(context.Background(), "run_decision_service", session.SessionID, turnIndex, "run tool", "run_decision_service"); err != nil {
		t.Fatalf("create bound run: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_decision_service",
		RunID:       "run_decision_service",
		Kind:        events.PendingActionKindElicitation,
		Subject:     "elicitation",
		PayloadJSON: `{"message":"Allow Acorn to continue?"}`,
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}

	record, err := NewPendingActionService(store).Decide(ctx, "action_decision_service", "accept")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Status != events.PendingActionStatusApproved {
		t.Fatalf("record status = %q, want approved", record.Status)
	}

	messages, err := store.ListSessionMessages(ctx, session.SessionID, 12)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	var parts []storesqlite.SessionMessagePart
	if err := json.Unmarshal([]byte(messages[1].ContentParts), &parts); err != nil {
		t.Fatalf("unmarshal message parts: %v", err)
	}
	if len(parts) == 0 || parts[0].Status != "approved" || parts[0].SelectedOptionID != "accept" {
		t.Fatalf("decision part = %#v", parts)
	}

	eventRecords, err := store.LoadEventsAfter(ctx, "run_decision_service", 0)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	var sawElicitationDecided bool
	for _, event := range eventRecords {
		if event.Kind == "elicitation.decided" {
			sawElicitationDecided = true
		}
	}
	if !sawElicitationDecided {
		t.Fatalf("expected elicitation.decided event, got %#v", eventRecords)
	}
}

func TestPendingActionServiceListAndGetProjectActionableRecords(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	session, err := store.CreateSession(ctx, "thread_pending_surface", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_pending_surface", session.SessionID, 1, "approve", "run_pending_surface"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_pending_surface",
		RunID:       "run_pending_surface",
		Kind:        events.PendingActionKindElicitation,
		Subject:     "Approval required",
		PayloadJSON: `{"message":"Allow Acorn to continue?","requested_schema":{"type":"object"}}`,
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
		Reason:      "needs owner approval",
		Rule:        "mobile_control",
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}

	service := NewPendingActionService(store)
	items, err := service.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].ActionID != "action_pending_surface" || items[0].ThreadID != "thread_pending_surface" || items[0].Body != "Allow Acorn to continue?" {
		t.Fatalf("summary = %#v", items[0])
	}

	detail, err := service.Get(ctx, "action_pending_surface")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.ActionID != "action_pending_surface" || detail.Reason != "needs owner approval" || detail.Rule != "mobile_control" {
		t.Fatalf("detail = %#v", detail)
	}
	if detail.Payload["message"] != "Allow Acorn to continue?" {
		t.Fatalf("detail payload = %#v", detail.Payload)
	}
}

func TestPendingActionServiceGetRejectsDecidedAction(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	session, err := store.CreateSession(ctx, "thread_decided_surface", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_decided_surface", session.SessionID, 1, "approve", "run_decided_surface"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_decided_surface",
		RunID:       "run_decided_surface",
		Kind:        events.PendingActionKindElicitation,
		PayloadJSON: `{"message":"Allow Acorn to continue?"}`,
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
	}); err != nil {
		t.Fatalf("create pending action: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_decided_surface", events.PendingActionStatusApproved, events.PendingActionModeDeferred, `{"action":"accept"}`); err != nil {
		t.Fatalf("decide pending action: %v", err)
	}

	_, err = NewPendingActionService(store).Get(ctx, "action_decided_surface")
	if !errors.Is(err, storecore.ErrPendingActionDecided) {
		t.Fatalf("Get error = %v, want ErrPendingActionDecided", err)
	}
}

func TestStatusToDecisionAction(t *testing.T) {
	t.Parallel()

	if got := statusToDecisionAction(events.PendingActionStatusApproved); got != "accept" {
		t.Fatalf("statusToDecisionAction(approved) = %q", got)
	}
	if got := statusToDecisionAction(events.PendingActionStatusRejected); got != "decline" {
		t.Fatalf("statusToDecisionAction(rejected) = %q", got)
	}
}
