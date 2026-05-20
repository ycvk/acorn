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

func (s *PendingActionService) Decide(ctx context.Context, actionID string, input PendingActionDecisionInput) (*events.PendingActionRecord, error) {
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

	record, err = s.store.DecidePendingAction(ctx, actionID, status, events.PendingActionModeDeferred, string(decisionJSON))
	if err != nil {
		return nil, err
	}
	if err := s.store.SyncDecisionMessageForPendingAction(ctx, actionID); err != nil {
		return nil, err
	}
	if _, err := s.store.AppendEventContext(ctx, record.RunID, eventKind, eventPayload); err != nil {
		return nil, fmt.Errorf("append %s event: %w", eventKind, err)
	}
	return record, nil
}

func buildPendingActionDecision(record events.PendingActionRecord, input PendingActionDecisionInput) (events.PendingActionStatus, []byte, string, map[string]any, error) {
	switch record.Kind {
	case events.PendingActionKindElicitation:
		return buildElicitationDecision(record, input)
	case events.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionDecision(record, input)
	default:
		return "", nil, "", nil, fmt.Errorf("unsupported pending action kind %q", record.Kind)
	}
}

func buildElicitationDecision(record events.PendingActionRecord, input PendingActionDecisionInput) (events.PendingActionStatus, []byte, string, map[string]any, error) {
	if strings.TrimSpace(input.SelectedOptionID) != "" || strings.TrimSpace(input.Answer) != "" {
		return "", nil, "", nil, fmt.Errorf("%w: elicitation accepts decision only", ErrPendingActionDecisionInvalid)
	}
	status, err := pendingActionDecisionStatus(input.Decision)
	if err != nil {
		return "", nil, "", nil, err
	}

	decisionJSON, err := json.Marshal(map[string]any{
		"action": statusToDecisionAction(status),
	})
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("marshal pending action decision: %w", err)
	}
	return status, decisionJSON, "elicitation.decided", map[string]any{
		"action_id": record.ActionID,
		"decision":  statusToDecisionAction(status),
	}, nil
}

func buildOperatorQuestionDecision(record events.PendingActionRecord, input PendingActionDecisionInput) (events.PendingActionStatus, []byte, string, map[string]any, error) {
	payload, err := operatorQuestionPayload(record)
	if err != nil {
		return "", nil, "", nil, err
	}
	action := strings.TrimSpace(strings.ToLower(input.Decision))
	selectedOptionID := strings.TrimSpace(input.SelectedOptionID)
	answer := strings.TrimSpace(input.Answer)

	var status events.PendingActionStatus
	switch action {
	case events.OperatorQuestionDecisionAnswer:
		status = events.PendingActionStatusApproved
		if err := validateOperatorAnswer(payload, selectedOptionID, answer); err != nil {
			return "", nil, "", nil, err
		}
	case events.OperatorQuestionDecisionDecline:
		status = events.PendingActionStatusRejected
		if selectedOptionID != "" || answer != "" {
			return "", nil, "", nil, fmt.Errorf("%w: declined operator question must not include selected_option_id or answer", ErrPendingActionDecisionInvalid)
		}
	default:
		return "", nil, "", nil, fmt.Errorf("%w: %q", ErrPendingActionDecisionInvalid, input.Decision)
	}

	decision := events.OperatorQuestionDecision{
		Action:           action,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
	}
	decisionJSON, err := json.Marshal(decision)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("marshal operator question decision: %w", err)
	}
	eventPayload := map[string]any{
		"action_id": record.ActionID,
		"question":  payload.Question,
		"decision":  action,
	}
	if selectedOptionID != "" {
		eventPayload["selected_option_id"] = selectedOptionID
	}
	if answer != "" {
		eventPayload["answer"] = answer
	}
	return status, decisionJSON, "operator_question.decided", eventPayload, nil
}

func validateOperatorAnswer(payload events.OperatorQuestionPayload, selectedOptionID string, answer string) error {
	if selectedOptionID == "" && answer == "" {
		return fmt.Errorf("%w: operator answer requires selected_option_id or answer", ErrPendingActionDecisionInvalid)
	}
	if answer != "" && !payload.AllowFreeform {
		return fmt.Errorf("%w: operator question does not allow freeform answer", ErrPendingActionDecisionInvalid)
	}
	if selectedOptionID == "" {
		if len(payload.Options) > 0 && !payload.AllowFreeform {
			return fmt.Errorf("%w: operator answer requires selected_option_id", ErrPendingActionDecisionInvalid)
		}
		return nil
	}
	for _, option := range payload.Options {
		if strings.TrimSpace(option.ID) == selectedOptionID {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown selected_option_id %q", ErrPendingActionDecisionInvalid, selectedOptionID)
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
