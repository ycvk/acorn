package api

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	optionsQuestion  = `{"question":"Which day?","options":[{"id":"mon","label":"Monday"},{"id":"tue","label":"Tuesday","description":"after lunch"}]}`
	freeformQuestion = `{"question":"What should the note be called?","allow_freeform":true}`
	mixedQuestion    = `{"question":"Which day?","options":[{"id":"mon","label":"Monday"}],"allow_freeform":true}`
)

type operatorQuestionStore struct {
	unimplementedStore
	record core.PendingActionRecord
	events []map[string]any
}

func (s *operatorQuestionStore) LoadPendingAction(_ context.Context, actionID string) (*core.PendingActionRecord, error) {
	if actionID != s.record.ActionID {
		return nil, core.ErrPendingActionNotFound
	}
	record := s.record
	return &record, nil
}

func (s *operatorQuestionStore) DecidePendingAction(_ context.Context, _ string, status core.PendingActionStatus, decisionJSON string) (*core.PendingActionRecord, error) {
	s.record.Status = status
	s.record.DecisionJSON = decisionJSON
	record := s.record
	return &record, nil
}

func (s *operatorQuestionStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	data, ok := payload.(map[string]any)
	if !ok {
		return core.EventRecord{}, errors.New("operator question event payload is not a map")
	}
	s.events = append(s.events, map[string]any{"kind": kind, "payload": data})
	return core.EventRecord{RunID: runID, Kind: kind, Payload: payload}, nil
}

func newOperatorQuestionRecord(payloadJSON string) core.PendingActionRecord {
	return core.PendingActionRecord{
		ActionID:    "action_00000000000000b1",
		RunID:       "run_1",
		Kind:        core.PendingActionKindOperatorQuestion,
		Subject:     "Pick a day",
		PayloadJSON: payloadJSON,
		Status:      core.PendingActionStatusPending,
		CreatedAt:   time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	}
}

func operatorInterrupt(actionID string) resumeInterruptContext {
	return resumeInterruptContext{
		ID:   "interrupt_1",
		Info: map[string]any{"kind": "operator_question", "action_id": actionID},
	}
}

func TestOperatorQuestionSummaryCarriesOptionsAndFreeform(t *testing.T) {
	run := core.RunRecord{RunID: "run_1", SessionID: "thread_1"}

	options, err := buildPendingActionSummary(newOperatorQuestionRecord(optionsQuestion), run)
	if err != nil {
		t.Fatalf("build options summary: %v", err)
	}
	if options.Kind != "operator_question" || options.Title != "Pick a day" || options.Body != "Which day?" || options.ThreadID != "thread_1" {
		t.Fatalf("unexpected summary header: %+v", options)
	}
	wantOptions := []PendingActionOption{{ID: "mon", Label: "Monday"}, {ID: "tue", Label: "Tuesday", Description: "after lunch"}}
	if !reflect.DeepEqual(options.Options, wantOptions) || options.AllowFreeform {
		t.Fatalf("options summary = %+v", options)
	}

	freeform, err := buildPendingActionSummary(newOperatorQuestionRecord(freeformQuestion), run)
	if err != nil {
		t.Fatalf("build freeform summary: %v", err)
	}
	if len(freeform.Options) != 0 || !freeform.AllowFreeform {
		t.Fatalf("freeform summary = %+v", freeform)
	}

	summaryJSON, err := json.Marshal(DefaultConverter.pendingActionSummaryDTOsFromDomain([]PendingActionSummary{freeform}))
	if err != nil {
		t.Fatalf("marshal summary dto: %v", err)
	}
	detailJSON, err := json.Marshal(pendingActionDetailDTOFromDomain(PendingActionDetail{PendingActionSummary: freeform}))
	if err != nil {
		t.Fatalf("marshal detail dto: %v", err)
	}
	for name, body := range map[string][]byte{"summary": summaryJSON, "detail": detailJSON} {
		if !strings.Contains(string(body), `"options":[]`) || !strings.Contains(string(body), `"allow_freeform":true`) {
			t.Fatalf("%s dto = %s, want an empty options array and allow_freeform", name, body)
		}
	}
}

func TestDecideOperatorQuestionResumesWithTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		question     string
		input        PendingActionDecisionInput
		wantStatus   core.PendingActionStatus
		wantDecision map[string]any
	}{
		{
			name:         "option",
			question:     optionsQuestion,
			input:        PendingActionDecisionInput{Decision: "answer", SelectedOptionID: "tue"},
			wantStatus:   core.PendingActionStatusApproved,
			wantDecision: map[string]any{"action": "answer", "selected_option_id": "tue"},
		},
		{
			name:         "freeform",
			question:     freeformQuestion,
			input:        PendingActionDecisionInput{Decision: "answer", Answer: "  Weekly review  "},
			wantStatus:   core.PendingActionStatusApproved,
			wantDecision: map[string]any{"action": "answer", "answer": "Weekly review"},
		},
		{
			name:         "freeform beside options",
			question:     mixedQuestion,
			input:        PendingActionDecisionInput{Decision: "answer", Answer: "Friday"},
			wantStatus:   core.PendingActionStatusApproved,
			wantDecision: map[string]any{"action": "answer", "answer": "Friday"},
		},
		{
			name:         "option beside freeform",
			question:     mixedQuestion,
			input:        PendingActionDecisionInput{Decision: "answer", SelectedOptionID: "mon"},
			wantStatus:   core.PendingActionStatusApproved,
			wantDecision: map[string]any{"action": "answer", "selected_option_id": "mon"},
		},
		{
			name:         "decline",
			question:     optionsQuestion,
			input:        PendingActionDecisionInput{Decision: "decline"},
			wantStatus:   core.PendingActionStatusRejected,
			wantDecision: map[string]any{"action": "decline"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &operatorQuestionStore{record: newOperatorQuestionRecord(tc.question)}
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
			if !reflect.DeepEqual(decision, tc.wantDecision) {
				t.Fatalf("decision = %v, want %v", decision, tc.wantDecision)
			}

			if len(store.events) != 1 || store.events[0]["kind"] != "operator_question.decided" {
				t.Fatalf("events = %v", store.events)
			}
			wantEvent := map[string]any{"action_id": record.ActionID, "question": questionText(t, tc.question), "decision": tc.wantDecision["action"]}
			for _, key := range []string{"selected_option_id", "answer"} {
				if value, ok := tc.wantDecision[key]; ok {
					wantEvent[key] = value
				}
			}
			if got := store.events[0]["payload"]; !reflect.DeepEqual(got, wantEvent) {
				t.Fatalf("event payload = %v, want %v", got, wantEvent)
			}

			targets, err := NewRunResumeService(store).resumeTargetsForContext(context.Background(), "run_1", operatorInterrupt(record.ActionID))
			if err != nil {
				t.Fatalf("resume targets: %v", err)
			}
			wantTarget := map[string]any{"action_id": record.ActionID}
			for key, value := range tc.wantDecision {
				wantTarget[key] = value
			}
			if !reflect.DeepEqual(targets, map[string]any{"interrupt_1": wantTarget}) {
				t.Fatalf("targets = %v, want %v", targets, wantTarget)
			}
		})
	}
}

func questionText(t *testing.T, payloadJSON string) string {
	t.Helper()
	var payload core.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("question payload: %v", err)
	}
	return payload.Question
}

func TestDecideOperatorQuestionRejectsInvalidDecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		question string
		input    PendingActionDecisionInput
	}{
		{"accept", optionsQuestion, PendingActionDecisionInput{Decision: "accept", SelectedOptionID: "mon"}},
		{"decline naming an option", optionsQuestion, PendingActionDecisionInput{Decision: "decline", SelectedOptionID: "mon"}},
		{"decline with an answer", freeformQuestion, PendingActionDecisionInput{Decision: "decline", Answer: "no"}},
		{"empty answer", optionsQuestion, PendingActionDecisionInput{Decision: "answer", Answer: "   "}},
		{"unknown option", optionsQuestion, PendingActionDecisionInput{Decision: "answer", SelectedOptionID: "wed"}},
		{"option on a freeform question", freeformQuestion, PendingActionDecisionInput{Decision: "answer", SelectedOptionID: "mon"}},
		{"freeform without allow_freeform", optionsQuestion, PendingActionDecisionInput{Decision: "answer", Answer: "Friday"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &operatorQuestionStore{record: newOperatorQuestionRecord(tc.question)}
			_, err := NewPendingActionService(store).Decide(context.Background(), store.record.ActionID, tc.input)
			if !errors.Is(err, ErrPendingActionDecisionInvalid) {
				t.Fatalf("err = %v, want ErrPendingActionDecisionInvalid", err)
			}
			if store.record.Status != core.PendingActionStatusPending || len(store.events) != 0 {
				t.Fatalf("invalid decision changed the action: status %q, events %v", store.record.Status, store.events)
			}
		})
	}
}

func TestOperatorQuestionResumeTargetsRequireADecidedAction(t *testing.T) {
	decided := func(mutate func(*core.PendingActionRecord)) core.PendingActionRecord {
		record := newOperatorQuestionRecord(optionsQuestion)
		record.Status = core.PendingActionStatusApproved
		record.DecisionJSON = `{"action":"answer","selected_option_id":"mon"}`
		mutate(&record)
		return record
	}
	for _, tc := range []struct {
		name      string
		record    core.PendingActionRecord
		interrupt resumeInterruptContext
		wantErr   string
	}{
		{"missing action id", decided(func(*core.PendingActionRecord) {}), resumeInterruptContext{ID: "interrupt_1", Info: map[string]any{"kind": "operator_question"}}, "missing action_id"},
		{"unknown action", decided(func(*core.PendingActionRecord) {}), operatorInterrupt("action_00000000000000ff"), core.ErrPendingActionNotFound.Error()},
		{"another run", decided(func(r *core.PendingActionRecord) { r.RunID = "run_2" }), operatorInterrupt("action_00000000000000b1"), "belongs to run run_2"},
		{"another kind", decided(func(r *core.PendingActionRecord) { r.Kind = core.PendingActionKindToolApproval }), operatorInterrupt("action_00000000000000b1"), `has kind "tool_approval"`},
		{"still pending", decided(func(r *core.PendingActionRecord) { r.Status = core.PendingActionStatusPending }), operatorInterrupt("action_00000000000000b1"), "still pending"},
		{"resolved", decided(func(r *core.PendingActionRecord) { r.Status = core.PendingActionStatusResolved }), operatorInterrupt("action_00000000000000b1"), `unsupported status "resolved"`},
		{"invalid decision json", decided(func(r *core.PendingActionRecord) { r.DecisionJSON = "" }), operatorInterrupt("action_00000000000000b1"), "invalid decision_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewRunResumeService(&operatorQuestionStore{record: tc.record})
			_, err := svc.resumeTargetsForContext(context.Background(), "run_1", tc.interrupt)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}
