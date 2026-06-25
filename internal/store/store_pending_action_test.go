package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/core"
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
	if err := store.CreateRun(ctx, core.RunCreateParams{
		RunID: "run_pa",
		Input: "test input",
	}); err != nil {
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

	record, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    "action_1",
		RunID:       runID,
		InterruptID: "interrupt_1",
		Kind:        core.PendingActionKindElicitation,
		Subject:     "test elicitation",
		PayloadJSON: `{"question":"what is 2+2?"}`,
		Status:      core.PendingActionStatusPending,
		Reason:      "model needs user input",
	})
	if err != nil {
		t.Fatalf("create pending action: %v", err)
	}
	if record.ActionID != "action_1" {
		t.Errorf("ActionID = %q, want action_1", record.ActionID)
	}
	if record.Kind != core.PendingActionKindElicitation {
		t.Errorf("Kind = %q, want %q", record.Kind, core.PendingActionKindElicitation)
	}
	if record.Status != core.PendingActionStatusPending {
		t.Errorf("Status = %q, want %q", record.Status, core.PendingActionStatusPending)
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
	if record.ResolvedAt != nil {
		t.Error("ResolvedAt should be nil for new pending action")
	}
}

func TestCreatePendingActionDuplicate(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	input := core.PendingActionInput{
		ActionID: "action_dup",
		RunID:    runID,
		Kind:     core.PendingActionKindOperatorQuestion,
		Subject:  "first",
	}
	if _, err := store.CreatePendingAction(ctx, input); err != nil {
		t.Fatalf("create first: %v", err)
	}
	_, err := store.CreatePendingAction(ctx, input)
	if !errors.Is(err, core.ErrPendingActionExists) {
		t.Fatalf("duplicate create error = %v, want core.ErrPendingActionExists", err)
	}
}

func TestCreatePendingActionInvalidKind(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.CreatePendingAction(ctx, core.PendingActionInput{
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

	_, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_no_run",
		RunID:    "nonexistent_run",
		Kind:     core.PendingActionKindElicitation,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent run, got nil")
	}
}

func TestCreatePendingActionDefaultStatus(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	record, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_default_status",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
		Subject:  "test",
		Status:   "",
	})
	if err != nil {
		t.Fatalf("create with empty status: %v", err)
	}
	if record.Status != core.PendingActionStatusPending {
		t.Errorf("Status = %q, want %q (default)", record.Status, core.PendingActionStatusPending)
	}
}

func TestLoadPendingAction(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    "action_load",
		RunID:       runID,
		InterruptID: "int_load",
		Kind:        core.PendingActionKindOperatorQuestion,
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
	if !errors.Is(err, core.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want core.ErrPendingActionNotFound", err)
	}
}

func TestLoadPendingActionByInterrupt(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    "action_by_int",
		RunID:       runID,
		InterruptID: "int_unique",
		Kind:        core.PendingActionKindElicitation,
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
	if !errors.Is(err, core.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want core.ErrPendingActionNotFound", err)
	}
}

func TestAttachPendingActionInterrupt(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_attach",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
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
	if !errors.Is(err, core.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want core.ErrPendingActionNotFound", err)
	}
}

func TestListPendingActions(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	for _, tc := range []struct {
		id   string
		kind core.PendingActionKind
	}{
		{"action_list_1", core.PendingActionKindElicitation},
		{"action_list_2", core.PendingActionKindOperatorQuestion},
		{"action_list_3", core.PendingActionKindElicitation},
	} {
		if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
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
		if a.Status != core.PendingActionStatusPending {
			t.Errorf("Status = %q, want %q", a.Status, core.PendingActionStatusPending)
		}
	}
}

func TestListPendingActionsExcludesDecided(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_pending",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create pending: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_decided",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create decided: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_decided", core.PendingActionStatusApproved, `{"answer":"yes"}`); err != nil {
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

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_run_1",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_run_2",
		RunID:    runID,
		Kind:     core.PendingActionKindOperatorQuestion,
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

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_decide_ok",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
		Subject:  "approve me",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	record, err := store.DecidePendingAction(ctx, "action_decide_ok", core.PendingActionStatusApproved, `{"answer":"yes"}`)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if record.Status != core.PendingActionStatusApproved {
		t.Errorf("Status = %q, want %q", record.Status, core.PendingActionStatusApproved)
	}
	if record.DecisionJSON != `{"answer":"yes"}` {
		t.Errorf("DecisionJSON = %q", record.DecisionJSON)
	}
	if record.ResolvedAt == nil {
		t.Error("ResolvedAt should not be nil after decide")
	}

	reloaded, err := store.LoadPendingAction(ctx, "action_decide_ok")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != core.PendingActionStatusApproved {
		t.Errorf("reloaded Status = %q, want %q", reloaded.Status, core.PendingActionStatusApproved)
	}
}

func TestDecidePendingActionReject(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_reject",
		RunID:    runID,
		Kind:     core.PendingActionKindOperatorQuestion,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	record, err := store.DecidePendingAction(ctx, "action_reject", core.PendingActionStatusRejected, `{"answer":"no"}`)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if record.Status != core.PendingActionStatusRejected {
		t.Errorf("Status = %q, want %q", record.Status, core.PendingActionStatusRejected)
	}
}

func TestDecidePendingActionAlreadyDecided(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_double",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_double", core.PendingActionStatusApproved, `{}`); err != nil {
		t.Fatalf("first decide: %v", err)
	}
	_, err := store.DecidePendingAction(ctx, "action_double", core.PendingActionStatusRejected, `{}`)
	if !errors.Is(err, core.ErrPendingActionDecided) {
		t.Fatalf("second decide error = %v, want core.ErrPendingActionDecided", err)
	}
}

func TestDecidePendingActionNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	_, err := store.DecidePendingAction(ctx, "nonexistent", core.PendingActionStatusApproved, `{}`)
	if !errors.Is(err, core.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want core.ErrPendingActionNotFound", err)
	}
}

func TestDecidePendingActionInvalidStatus(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_invalid_status",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := store.DecidePendingAction(ctx, "action_invalid_status", core.PendingActionStatusPending, `{}`)
	if err == nil {
		t.Fatal("expected error for invalid decision status (pending), got nil")
	}
	_, err = store.DecidePendingAction(ctx, "action_invalid_status", core.PendingActionStatusResolved, `{}`)
	if err == nil {
		t.Fatal("expected error for invalid decision status (resolved), got nil")
	}
}

func TestDecidePendingActionAppendsEvent(t *testing.T) {
	store, runID := setupPendingActionStore(t)
	ctx := context.Background()

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    "action_event",
		RunID:       runID,
		InterruptID: "int_event",
		Kind:        core.PendingActionKindElicitation,
		Subject:     "event test",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := store.DecidePendingAction(ctx, "action_event", core.PendingActionStatusApproved, `{"answer":"ok"}`); err != nil {
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

	if _, err := store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID: "action_resolve",
		RunID:    runID,
		Kind:     core.PendingActionKindElicitation,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.DecidePendingAction(ctx, "action_resolve", core.PendingActionStatusApproved, `{}`); err != nil {
		t.Fatalf("decide: %v", err)
	}

	if err := store.ResolvePendingAction(ctx, "action_resolve"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	record, err := store.LoadPendingAction(ctx, "action_resolve")
	if err != nil {
		t.Fatalf("load after resolve: %v", err)
	}
	if record.Status != core.PendingActionStatusResolved {
		t.Errorf("Status = %q, want %q", record.Status, core.PendingActionStatusResolved)
	}
	if record.ResolvedAt == nil {
		t.Error("ResolvedAt should not be nil after resolve")
	}
}

func TestResolvePendingActionNotFound(t *testing.T) {
	store, _ := setupPendingActionStore(t)
	ctx := context.Background()

	err := store.ResolvePendingAction(ctx, "nonexistent")
	if !errors.Is(err, core.ErrPendingActionNotFound) {
		t.Fatalf("error = %v, want core.ErrPendingActionNotFound", err)
	}
}

func TestNormalizePendingActionKind(t *testing.T) {
	tests := []struct {
		input core.PendingActionKind
		want  core.PendingActionKind
		err   bool
	}{
		{core.PendingActionKindElicitation, core.PendingActionKindElicitation, false},
		{core.PendingActionKindOperatorQuestion, core.PendingActionKindOperatorQuestion, false},
		{"  elicitation  ", core.PendingActionKindElicitation, false},
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
		input core.PendingActionStatus
		want  core.PendingActionStatus
		err   bool
	}{
		{"", core.PendingActionStatusPending, false},
		{core.PendingActionStatusPending, core.PendingActionStatusPending, false},
		{core.PendingActionStatusApproved, core.PendingActionStatusApproved, false},
		{core.PendingActionStatusRejected, core.PendingActionStatusRejected, false},
		{core.PendingActionStatusResolved, core.PendingActionStatusResolved, false},
		{"  approved  ", core.PendingActionStatusApproved, false},
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
		input core.PendingActionStatus
		want  core.PendingActionStatus
		err   bool
	}{
		{core.PendingActionStatusApproved, core.PendingActionStatusApproved, false},
		{core.PendingActionStatusRejected, core.PendingActionStatusRejected, false},
		{"  approved  ", core.PendingActionStatusApproved, false},
		{core.PendingActionStatusPending, "", true},
		{core.PendingActionStatusResolved, "", true},
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
