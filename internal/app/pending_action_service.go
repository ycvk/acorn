package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
)

var ErrPendingActionDecisionInvalid = errors.New("pending action decision invalid")

type PendingActionService struct {
	store containerAppStore
}

func NewPendingActionService(store containerAppStore) *PendingActionService {
	return &PendingActionService{store: store}
}

type PendingActionDetail struct {
	PendingActionSummary
	Payload map[string]any
	Reason  string
	Rule    string
}

type PendingActionDecisionInput struct {
	Decision         string
	SelectedOptionID string
	Answer           string
}

func (s *PendingActionService) List(ctx context.Context, limit int) ([]PendingActionSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	records, err := s.store.ListPendingActions(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]PendingActionSummary, 0, len(records))
	for _, record := range records {
		run, err := s.store.LoadRun(ctx, record.RunID)
		if err != nil {
			return nil, err
		}
		summary, err := buildPendingActionSummary(record, *run)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

func (s *PendingActionService) Get(ctx context.Context, actionID string) (*PendingActionDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("pending action id is required")
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.Status != domain.PendingActionStatusPending {
		return nil, fmt.Errorf("%w: status %q", store.ErrPendingActionDecided, record.Status)
	}
	run, err := s.store.LoadRun(ctx, record.RunID)
	if err != nil {
		return nil, err
	}
	summary, err := buildPendingActionSummary(*record, *run)
	if err != nil {
		return nil, err
	}
	payload, err := pendingActionPayload(*record)
	if err != nil {
		return nil, err
	}
	return &PendingActionDetail{
		PendingActionSummary: summary,
		Payload:              payload,
		Reason:               record.Reason,
		Rule:                 "",
	}, nil
}

func (s *PendingActionService) Decide(ctx context.Context, actionID string, input PendingActionDecisionInput) (*domain.PendingActionRecord, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("pending action id is required")
	}

	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}

	status, decisionJSON, eventKind, eventPayload, err := buildPendingActionDecision(*record, input)
	if err != nil {
		return nil, err
	}

	record, err = s.store.DecidePendingAction(ctx, actionID, status, string(decisionJSON))
	if err != nil {
		return nil, err
	}
	if _, err := s.store.AppendEventContext(ctx, record.RunID, eventKind, eventPayload); err != nil {
		return nil, fmt.Errorf("append %s event: %w", eventKind, err)
	}
	return record, nil
}

func buildPendingActionDecision(record domain.PendingActionRecord, input PendingActionDecisionInput) (domain.PendingActionStatus, []byte, string, map[string]any, error) {
	switch record.Kind {
	case domain.PendingActionKindElicitation:
		return buildElicitationDecision(record, input)
	case domain.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionDecision(record, input)
	default:
		return "", nil, "", nil, fmt.Errorf("unsupported pending action kind %q", record.Kind)
	}
}
