package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/domain"
	storecore "github.com/ycvk/acorn/internal/store"
)

func setupPendingActionStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.CreateRun(ctx, "run_pa", "test input"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.CreateSession(ctx, "sess_pa", "test"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return store, "run_pa"
}

func TestCreatePendingAction(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	record, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_1",
		RunID:       runID,
		InterruptID: "interrupt_1",
		Kind:        domain.PendingActionKindElicitation,
		Subject:     "test elicitation",
		PayloadJSON: `{"question":"what is 2+2?"}`,
		Status:      domain.PendingActionStatusPending,
		Reason:      "model needs user input",
	})
	if err != nil {
		t.Fatalf("create pending action: %v", err)
	}
	if record.ActionID != "action_1" {
		t.Errorf("ActionID = %q, want action_1", record.ActionID)
	}
	if record.Kind != domain.PendingActionKindElicitation {
		t.Errorf("Kind = %q, want %q", record.Kind, domain.PendingActionKindElicitation)
	}
	if record.Status != domain.PendingActionStatusPending {
		t.Errorf("Status = %q, want %q", record.Status, domain.PendingActionStatusPending)
	}
	if record.Subject != "test elicitation" {
		t.Errorf("Subject = %q, want test elicitation", record.Subject)
	}
	if record.Reason != "model needs user input" {
		t.Errorf("Reason = %q, want model needs user input", record.Reason)
	}
	if record.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if record.DecidedAt != nil {
		t.Error("DecidedAt should be nil for new pending action")
	}
}

func TestCreatePendingActionDuplicate(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	input := storecore.CreatePendingActionInput{
		ActionID: "action_dup",
		RunID:    runID,
		Kind:     domain.PendingActionKindOperatorQuestion,
		Subject:  "first",
	}
	if _, err := store.CreatePendingAction(ctx, input); err != nil {
		t.Fatalf("create first: %v", err)
	}
	_, err := store.CreatePendingAction(ctx, input)
	if !errors.Is(err, storecore.ErrPendingActionExists) {
		t.Fatalf("duplicate create error = %v, want ErrPendingActionExists", err)
	}
}

func TestCreatePendingActionInvalidKind(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_bad_kind",
		RunID:    runID,
		Kind:     "invalid_kind",
	})
	if err == nil {
		t.Fatal("expected error for invalid kind, got nil")
	}
}

func TestCreatePendingActionNonexistentRun(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_no_run",
		RunID:    "nonexistent_run",
		Kind:     domain.PendingActionKindElicitation,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent run, got nil")
	}
}

func TestCreatePendingActionDefaultStatus(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	record, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_default_status",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
		Subject:  "test",
		Status:   "",
	})
	if err != nil {
		t.Fatalf("create with empty status: %v", err)
	}
	if record.Status != domain.PendingActionStatusPending {
		t.Errorf("Status = %q, want %q (default)", record.Status, domain.PendingActionStatusPending)
	}
}

func TestLoadPendingAction(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_load",
		RunID:       runID,
		InterruptID: "int_load",
		Kind:        domain.PendingActionKindOperatorQuestion,
		Subject:     "load test",
		PayloadJSON: `{"q":"hello"}`,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	record, err := store.LoadPendingAction(ctx, "action_load")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if record.ActionID != "action_load" {
		t.Errorf("ActionID = %q", record.ActionID)
	}
	if record.InterruptID != "int_load" {
		t.Errorf("InterruptID = %q, want int_load", record.InterruptID)
	}
}

func TestLoadPendingActionNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.LoadPendingAction(ctx, "nonexistent")
	if !errors.Is(err, storecore.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want ErrPendingActionNotFound", err)
	}
}

func TestLoadPendingActionByInterrupt(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_by_int",
		RunID:       runID,
		InterruptID: "int_unique",
		Kind:        domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	record, err := store.LoadPendingActionByInterrupt(ctx, "int_unique")
	if err != nil {
		t.Fatalf("load by interrupt: %v", err)
	}
	if record.ActionID != "action_by_int" {
		t.Errorf("ActionID = %q, want action_by_int", record.ActionID)
	}
}

func TestLoadPendingActionByInterruptNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.LoadPendingActionByInterrupt(ctx, "nonexistent_int")
	if !errors.Is(err, storecore.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want ErrPendingActionNotFound", err)
	}
}

func TestAttachPendingActionInterrupt(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_attach",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.AttachPendingActionInterrupt(ctx, "action_attach", "int_attached"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	record, err := store.LoadPendingAction(ctx, "action_attach")
	if err != nil {
		t.Fatalf("load after attach: %v", err)
	}
	if record.InterruptID != "int_attached" {
		t.Errorf("InterruptID = %q, want int_attached", record.InterruptID)
	}
}

func TestAttachPendingActionInterruptNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	err := store.AttachPendingActionInterrupt(ctx, "nonexistent", "int_x")
	if !errors.Is(err, storecore.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want ErrPendingActionNotFound", err)
	}
}

func TestListPendingActions(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	for _, tc := range []struct {
		id   string
		kind domain.PendingActionKind
	}{
		{"action_list_1", domain.PendingActionKindElicitation},
		{"action_list_2", domain.PendingActionKindOperatorQuestion},
		{"action_list_3", domain.PendingActionKindElicitation},
	} {
		if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
			ActionID: tc.id,
			RunID:    runID,
			Kind:     tc.kind,
			Subject:  tc.id,
		}); err != nil {
			t.Fatalf("create %s: %v", tc.id, err)
		}
	}

	actions, err := store.ListPendingActions(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(actions) != 3 {
		t.Fatalf("len = %d, want 3", len(actions))
	}
	for _, a := range actions {
		if a.Status != domain.PendingActionStatusPending {
			t.Errorf("Status = %q, want %q", a.Status, domain.PendingActionStatusPending)
		}
	}
}

func TestListPendingActionsExcludesDecided(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_pending",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create pending: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_decided",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create decided: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_decided", domain.PendingActionStatusApproved, `{"answer":"yes"}`); err != nil {
		t.Fatalf("decide: %v", err)
	}

	actions, err := store.ListPendingActions(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("len = %d, want 1 (only pending)", len(actions))
	}
	if actions[0].ActionID != "action_pending" {
		t.Errorf("ActionID = %q, want action_pending", actions[0].ActionID)
	}
}

func TestListPendingActionsByRun(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_run_1",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_run_2",
		RunID:    runID,
		Kind:     domain.PendingActionKindOperatorQuestion,
	}); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	actions, err := store.ListPendingActionsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("list by run: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("len = %d, want 2", len(actions))
	}
}

func TestListPendingActionsByRunEmpty(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	actions, err := store.ListPendingActionsByRun(ctx, "nonexistent_run")
	if err != nil {
		t.Fatalf("list by run: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("len = %d, want 0", len(actions))
	}
}

func TestDecidePendingActionApprove(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_decide_ok",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
		Subject:  "approve me",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	record, err := store.DecidePendingAction(ctx, "action_decide_ok", domain.PendingActionStatusApproved, `{"answer":"yes"}`)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if record.Status != domain.PendingActionStatusApproved {
		t.Errorf("Status = %q, want %q", record.Status, domain.PendingActionStatusApproved)
	}
	if record.DecisionJSON != `{"answer":"yes"}` {
		t.Errorf("DecisionJSON = %q", record.DecisionJSON)
	}
	if record.DecidedAt == nil {
		t.Error("DecidedAt should not be nil after decide")
	}

	reloaded, err := store.LoadPendingAction(ctx, "action_decide_ok")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != domain.PendingActionStatusApproved {
		t.Errorf("reloaded Status = %q, want %q", reloaded.Status, domain.PendingActionStatusApproved)
	}
}

func TestDecidePendingActionReject(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_reject",
		RunID:    runID,
		Kind:     domain.PendingActionKindOperatorQuestion,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	record, err := store.DecidePendingAction(ctx, "action_reject", domain.PendingActionStatusRejected, `{"answer":"no"}`)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if record.Status != domain.PendingActionStatusRejected {
		t.Errorf("Status = %q, want %q", record.Status, domain.PendingActionStatusRejected)
	}
}

func TestDecidePendingActionAlreadyDecided(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_double",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_double", domain.PendingActionStatusApproved, `{}`); err != nil {
		t.Fatalf("first decide: %v", err)
	}
	_, err := store.DecidePendingAction(ctx, "action_double", domain.PendingActionStatusRejected, `{}`)
	if !errors.Is(err, storecore.ErrPendingActionDecided) {
		t.Fatalf("second decide error = %v, want ErrPendingActionDecided", err)
	}
}

func TestDecidePendingActionNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.DecidePendingAction(ctx, "nonexistent", domain.PendingActionStatusApproved, `{}`)
	if !errors.Is(err, storecore.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want ErrPendingActionNotFound", err)
	}
}

func TestDecidePendingActionInvalidStatus(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_invalid_status",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := store.DecidePendingAction(ctx, "action_invalid_status", domain.PendingActionStatusPending, `{}`)
	if err == nil {
		t.Fatal("expected error for invalid decision status (pending), got nil")
	}
	_, err = store.DecidePendingAction(ctx, "action_invalid_status", domain.PendingActionStatusResolved, `{}`)
	if err == nil {
		t.Fatal("expected error for invalid decision status (resolved), got nil")
	}
}

func TestDecidePendingActionAppendsEvent(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    "action_event",
		RunID:       runID,
		InterruptID: "int_event",
		Kind:        domain.PendingActionKindElicitation,
		Subject:     "event test",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := store.DecidePendingAction(ctx, "action_event", domain.PendingActionStatusApproved, `{"answer":"ok"}`); err != nil {
		t.Fatalf("decide: %v", err)
	}

	events, err := store.LoadEvents(ctx, runID)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "action.decided" {
			found = true
			break
		}
	}
	if !found {
		t.Error("action.decided event not found in run events")
	}
}

func TestResolvePendingAction(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID: "action_resolve",
		RunID:    runID,
		Kind:     domain.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_resolve", domain.PendingActionStatusApproved, `{}`); err != nil {
		t.Fatalf("decide: %v", err)
	}

	if err := store.ResolvePendingAction(ctx, "action_resolve"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	record, err := store.LoadPendingAction(ctx, "action_resolve")
	if err != nil {
		t.Fatalf("load after resolve: %v", err)
	}
	if record.Status != domain.PendingActionStatusResolved {
		t.Errorf("Status = %q, want %q", record.Status, domain.PendingActionStatusResolved)
	}
	if record.ResolvedAt == nil {
		t.Error("ResolvedAt should not be nil after resolve")
	}
}

func TestResolvePendingActionNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	err := store.ResolvePendingAction(ctx, "nonexistent")
	if !errors.Is(err, storecore.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want ErrPendingActionNotFound", err)
	}
}

func TestNormalizePendingActionKind(t *testing.T) {
	tests := []struct {
		input domain.PendingActionKind
		want  domain.PendingActionKind
		err   bool
	}{
		{domain.PendingActionKindElicitation, domain.PendingActionKindElicitation, false},
		{domain.PendingActionKindOperatorQuestion, domain.PendingActionKindOperatorQuestion, false},
		{"  elicitation  ", domain.PendingActionKindElicitation, false},
		{"invalid", "", true},
		{"", "", true},
	}
	for _, tc := range tests {
		got, err := normalizePendingActionKind(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("normalizePendingActionKind(%q) expected error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizePendingActionKind(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("normalizePendingActionKind(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizePendingActionStatus(t *testing.T) {
	tests := []struct {
		input domain.PendingActionStatus
		want  domain.PendingActionStatus
		err   bool
	}{
		{"", domain.PendingActionStatusPending, false},
		{domain.PendingActionStatusPending, domain.PendingActionStatusPending, false},
		{domain.PendingActionStatusApproved, domain.PendingActionStatusApproved, false},
		{domain.PendingActionStatusRejected, domain.PendingActionStatusRejected, false},
		{domain.PendingActionStatusResolved, domain.PendingActionStatusResolved, false},
		{"  approved  ", domain.PendingActionStatusApproved, false},
		{"invalid", "", true},
	}
	for _, tc := range tests {
		got, err := normalizePendingActionStatus(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("normalizePendingActionStatus(%q) expected error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizePendingActionStatus(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("normalizePendingActionStatus(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizePendingActionDecision(t *testing.T) {
	tests := []struct {
		input domain.PendingActionStatus
		want  domain.PendingActionStatus
		err   bool
	}{
		{domain.PendingActionStatusApproved, domain.PendingActionStatusApproved, false},
		{domain.PendingActionStatusRejected, domain.PendingActionStatusRejected, false},
		{"  approved  ", domain.PendingActionStatusApproved, false},
		{domain.PendingActionStatusPending, "", true},
		{domain.PendingActionStatusResolved, "", true},
		{"invalid", "", true},
	}
	for _, tc := range tests {
		got, err := normalizePendingActionDecision(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("normalizePendingActionDecision(%q) expected error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizePendingActionDecision(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("normalizePendingActionDecision(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
