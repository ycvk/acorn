package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type pendingActionServiceStore struct {
	unimplementedStore
	stub        *pendingActionHandlerStub
	record      core.PendingActionRecord
	summaries   []PendingActionSummary
	detail      *PendingActionDetail
	err         error
	actionID    string
	getActionID string
	decision    PendingActionDecisionInput
	listLimit   int
	decisionMap map[string]PendingActionDecisionInput
}

func newPendingActionTestService(stub *pendingActionHandlerStub) *PendingActionService {
	if stub == nil {
		stub = &pendingActionHandlerStub{}
	}
	record := stub.record
	if strings.TrimSpace(record.ActionID) == "" {
		record.ActionID = "action_1"
	}
	if strings.TrimSpace(record.RunID) == "" {
		record.RunID = "run_1"
	}
	if record.Kind == "" {
		record.Kind = core.PendingActionKindElicitation
	}
	if record.Status == "" {
		record.Status = core.PendingActionStatusPending
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	}
	store := &pendingActionServiceStore{
		stub:      stub,
		record:    record,
		summaries: append([]PendingActionSummary(nil), stub.summaries...),
		detail:    stub.detail,
		err:       stub.err,
		decisionMap: map[string]PendingActionDecisionInput{
			`{"action":"accept"}`:  {Decision: "accept"},
			`{"action":"decline"}`: {Decision: "decline"},
			`{"action":"maybe"}`:   {Decision: "maybe"},
		},
	}
	return NewPendingActionService(store)
}

func (s *pendingActionServiceStore) loadRunForThread(runID, threadID string) core.RunRecord {
	return core.RunRecord{
		RunID:     runID,
		SessionID: threadID,
		Status:    core.RunStatusRunning,
		Input:     "",
		CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}
}

func pendingActionRecordFromSummary(item PendingActionSummary) core.PendingActionRecord {
	return core.PendingActionRecord{
		ActionID: item.ActionID,
		RunID:    item.RunID,
		Kind:     core.PendingActionKind(item.Kind),
		Subject:  item.Title,
		PayloadJSON: func() string {
			if strings.TrimSpace(item.Body) == "" {
				return ""
			}
			return fmt.Sprintf(`{"message":%q}`, item.Body)
		}(),
		Status:    core.PendingActionStatus(item.Status),
		CreatedAt: item.CreatedAt,
	}
}

func (s *pendingActionServiceStore) ListPendingActions(_ context.Context, limit int) ([]core.PendingActionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.listLimit = limit
	if s.stub != nil {
		s.stub.listLimit = limit
	}
	out := make([]core.PendingActionRecord, 0, len(s.summaries))
	for _, item := range s.summaries {
		out = append(out, pendingActionRecordFromSummary(item))
	}
	return out, nil
}

func (s *pendingActionServiceStore) LoadPendingAction(_ context.Context, actionID string) (*core.PendingActionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.getActionID = actionID
	if s.stub != nil {
		s.stub.getActionID = actionID
	}
	if strings.TrimSpace(actionID) == "" {
		return nil, core.ErrPendingActionNotFound
	}
	if s.detail != nil {
		record := pendingActionRecordFromSummary(s.detail.PendingActionSummary)
		record.Reason = s.detail.Reason
		if len(s.detail.Payload) > 0 {
			record.PayloadJSON = fmt.Sprintf(`{"message":%q}`, s.detail.Payload["message"])
		}
		return &record, nil
	}
	record := s.record
	if strings.TrimSpace(record.ActionID) == "" {
		record = core.PendingActionRecord{
			ActionID:    actionID,
			RunID:       "run_1",
			Kind:        core.PendingActionKindElicitation,
			Status:      core.PendingActionStatusPending,
			CreatedAt:   time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			PayloadJSON: `{"message":"Approval required"}`,
		}
	}
	return &record, nil
}

func (s *pendingActionServiceStore) DecidePendingAction(_ context.Context, actionID string, status core.PendingActionStatus, decisionJSON string) (*core.PendingActionRecord, error) {
	s.actionID = actionID
	if s.stub != nil {
		s.stub.actionID = actionID
	}
	if item, ok := s.decisionMap[decisionJSON]; ok {
		s.decision = item
		if s.stub != nil {
			s.stub.decision = item
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	record := s.record
	record.ActionID = actionID
	record.Status = status
	record.DecisionJSON = decisionJSON
	return &record, nil
}

func (s *pendingActionServiceStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.detail != nil {
		record := s.loadRunForThread(runID, s.detail.ThreadID)
		return &record, nil
	}
	if len(s.summaries) > 0 {
		record := s.loadRunForThread(runID, s.summaries[0].ThreadID)
		return &record, nil
	}
	record := s.loadRunForThread(runID, "thread_1")
	return &record, nil
}

func (s *pendingActionServiceStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s *pendingActionServiceStore) LoadEventsAfter(context.Context, string, int64) ([]core.EventRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s *pendingActionServiceStore) AppendEvent(context.Context, string, string, any) (core.EventRecord, error) {
	if s.err != nil {
		return core.EventRecord{}, s.err
	}
	return core.EventRecord{Sequence: 1}, nil
}
