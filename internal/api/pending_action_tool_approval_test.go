package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type toolApprovalStore struct {
	unimplementedStore
	record     core.PendingActionRecord
	eventKinds []string
}

func (s *toolApprovalStore) LoadPendingAction(_ context.Context, actionID string) (*core.PendingActionRecord, error) {
	if actionID != s.record.ActionID {
		return nil, core.ErrPendingActionNotFound
	}
	record := s.record
	return &record, nil
}

func (s *toolApprovalStore) DecidePendingAction(_ context.Context, _ string, status core.PendingActionStatus, decisionJSON string) (*core.PendingActionRecord, error) {
	s.record.Status = status
	s.record.DecisionJSON = decisionJSON
	record := s.record
	return &record, nil
}

func (s *toolApprovalStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	s.eventKinds = append(s.eventKinds, kind)
	return core.EventRecord{RunID: runID, Kind: kind, Payload: payload}, nil
}

func newToolApprovalRecord() core.PendingActionRecord {
	return core.PendingActionRecord{
		ActionID:    "action_00000000000000a1",
		RunID:       "run_1",
		Kind:        core.PendingActionKindToolApproval,
		Subject:     "Approve recall",
		PayloadJSON: `{"message":"recall wants to run with arguments:\n{\"query\":\"x\"}","tool_name":"recall","arguments":"{\"query\":\"x\"}"}`,
		Status:      core.PendingActionStatusPending,
		CreatedAt:   time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC),
	}
}

func TestToolApprovalSummaryOffersAcceptAndDecline(t *testing.T) {
	summary, err := buildPendingActionSummary(newToolApprovalRecord(), core.RunRecord{RunID: "run_1", SessionID: "thread_1"})
	if err != nil {
		t.Fatalf("build summary: %v", err)
	}
	if summary.Kind != "tool_approval" || summary.Title != "Approve recall" || summary.ThreadID != "thread_1" {
		t.Fatalf("unexpected summary header: %+v", summary)
	}
	if summary.Body != "recall wants to run with arguments:\n{\"query\":\"x\"}" {
		t.Fatalf("body = %q", summary.Body)
	}
	if len(summary.Options) != 2 || summary.Options[0].ID != "accept" || summary.Options[1].ID != "decline" {
		t.Fatalf("options = %+v", summary.Options)
	}
}

func TestDecideToolApprovalRecordsDecision(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      PendingActionDecisionInput
		wantStatus core.PendingActionStatus
		wantAction string
	}{
		{"accept", PendingActionDecisionInput{Decision: "accept"}, core.PendingActionStatusApproved, "accept"},
		{"accept with option id", PendingActionDecisionInput{Decision: "accept", SelectedOptionID: "accept"}, core.PendingActionStatusApproved, "accept"},
		{"decline", PendingActionDecisionInput{Decision: "decline", SelectedOptionID: "decline"}, core.PendingActionStatusRejected, "decline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &toolApprovalStore{record: newToolApprovalRecord()}
			record, err := NewPendingActionService(store).Decide(context.Background(), store.record.ActionID, tc.input)
			if err != nil {
				t.Fatalf("decide: %v", err)
			}
			if record.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", record.Status, tc.wantStatus)
			}
			var decision map[string]any
			if err := json.Unmarshal([]byte(record.DecisionJSON), &decision); err != nil {
				t.Fatalf("decision json: %v", err)
			}
			if decision["action"] != tc.wantAction {
				t.Fatalf("decision = %v", decision)
			}
			if len(store.eventKinds) != 1 || store.eventKinds[0] != "tool_approval.decided" {
				t.Fatalf("events = %v", store.eventKinds)
			}
		})
	}
}

func TestDecideToolApprovalRejectsMismatchedOption(t *testing.T) {
	store := &toolApprovalStore{record: newToolApprovalRecord()}
	_, err := NewPendingActionService(store).Decide(context.Background(), store.record.ActionID, PendingActionDecisionInput{Decision: "accept", SelectedOptionID: "decline"})
	if err == nil {
		t.Fatal("expected mismatched option id to be rejected")
	}
}

func TestToolApprovalResumeTargetsCarryDecision(t *testing.T) {
	record := newToolApprovalRecord()
	record.Status = core.PendingActionStatusApproved
	record.DecisionJSON = `{"action":"accept"}`
	svc := NewRunResumeService(&toolApprovalStore{record: record})
	targets, err := svc.resumeTargetsForContext(context.Background(), "run_1", resumeInterruptContext{
		ID:   "interrupt_1",
		Info: map[string]any{"kind": "tool_approval", "action_id": record.ActionID},
	})
	if err != nil {
		t.Fatalf("resume targets: %v", err)
	}
	data, ok := targets["interrupt_1"].(map[string]any)
	if !ok || data["action"] != "accept" || data["action_id"] != record.ActionID {
		t.Fatalf("targets = %v", targets)
	}
}

func TestToolApprovalResumeTargetsRejectPendingAction(t *testing.T) {
	svc := NewRunResumeService(&toolApprovalStore{record: newToolApprovalRecord()})
	_, err := svc.resumeTargetsForContext(context.Background(), "run_1", resumeInterruptContext{
		ID:   "interrupt_1",
		Info: map[string]any{"kind": "tool_approval", "action_id": "action_00000000000000a1"},
	})
	if err == nil {
		t.Fatal("expected a still-pending approval to block resume")
	}
}
