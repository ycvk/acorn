package api

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

type deviceAuthHandlerStub struct {
	authErr         error
	pairErr         error
	listErr         error
	revokeErr       error
	devices         []DeviceView
	revokedDeviceID string
}

type pendingActionHandlerStub struct {
	record      core.PendingActionRecord
	summaries   []PendingActionSummary
	detail      *PendingActionDetail
	err         error
	actionID    string
	getActionID string
	decision    PendingActionDecisionInput
	listLimit   int
}

func (s *pendingActionHandlerStub) List(_ context.Context, limit int) ([]PendingActionSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.listLimit = limit
	return append([]PendingActionSummary(nil), s.summaries...), nil
}

func (s *pendingActionHandlerStub) Get(_ context.Context, actionID string) (*PendingActionDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.getActionID = actionID
	return s.detail, nil
}

func (s *pendingActionHandlerStub) Decide(_ context.Context, actionID string, decision PendingActionDecisionInput) (*core.PendingActionRecord, error) {
	s.actionID = actionID
	s.decision = decision
	if s.err != nil {
		return nil, s.err
	}
	return &s.record, nil
}
