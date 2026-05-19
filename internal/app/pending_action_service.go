package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	storecore "github.com/ycvk/acorn/internal/store"
)

var ErrPendingActionDecisionInvalid = errors.New("pending action decision invalid")

type PendingActionService struct {
	store pendingActionDecisionStore
}

func NewPendingActionService(store pendingActionDecisionStore) *PendingActionService {
	return &PendingActionService{store: store}
}

type PendingActionDetail struct {
	PendingActionSummary
	Payload map[string]any
	Reason  string
	Rule    string
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
	if record.Status != events.PendingActionStatusPending {
		return nil, fmt.Errorf("%w: status %q", storecore.ErrPendingActionDecided, record.Status)
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
		Rule:                 record.Rule,
	}, nil
}

func (s *PendingActionService) Decide(ctx context.Context, actionID, decision string) (*events.PendingActionRecord, error) {
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
	if record.Kind != events.PendingActionKindElicitation {
		return nil, fmt.Errorf("unsupported pending action kind %q", record.Kind)
	}

	status, err := pendingActionDecisionStatus(decision)
	if err != nil {
		return nil, err
	}

	decisionJSON, err := json.Marshal(map[string]any{
		"action": statusToDecisionAction(status),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal pending action decision: %w", err)
	}

	record, err = s.store.DecidePendingAction(ctx, actionID, status, events.PendingActionModeDeferred, string(decisionJSON))
	if err != nil {
		return nil, err
	}
	if err := s.store.SyncDecisionMessageForPendingAction(ctx, actionID); err != nil {
		return nil, err
	}
	if _, err := s.store.AppendEventContext(ctx, record.RunID, "elicitation.decided", map[string]any{
		"action_id": actionID,
		"decision":  statusToDecisionAction(status),
	}); err != nil {
		return nil, fmt.Errorf("append elicitation decided event: %w", err)
	}
	return record, nil
}

func pendingActionDecisionStatus(decision string) (events.PendingActionStatus, error) {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept":
		return events.PendingActionStatusApproved, nil
	case "decline":
		return events.PendingActionStatusRejected, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrPendingActionDecisionInvalid, decision)
	}
}

func statusToDecisionAction(status events.PendingActionStatus) string {
	switch status {
	case events.PendingActionStatusApproved:
		return "accept"
	case events.PendingActionStatusRejected:
		return "decline"
	default:
		return "decline"
	}
}
