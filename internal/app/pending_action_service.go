package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/store"
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

const (
	defaultInboxPendingActionLimit = 20
	defaultInboxActiveRunLimit     = 20
	defaultInboxTerminalRunLimit   = 20
	runSummaryPreviewMaxRunes      = 96
	runSummaryTitleMaxRunes        = 64
)

type InboxService struct {
	store        inboxStore
	capabilities inboxCapabilityService
}

type inboxCapabilityService interface {
	Snapshot(ctx context.Context, opts CapabilitySnapshotOptions) SystemCapabilities
}

type MobileInbox struct {
	PendingActions     []PendingActionSummary
	ActiveRuns         []RunSummary
	RecentTerminalRuns []RunSummary
	System             SystemCapabilities
}

type RunSummary struct {
	RunID          string
	ThreadID       string
	ThreadTitle    string
	Status         string
	Mode           string
	Preview        string
	LastEventLabel string
	AttentionLevel string
	DurationMS     int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PendingActionSummary struct {
	ActionID  string
	RunID     string
	ThreadID  string
	Kind      string
	Status    string
	Title     string
	Body      string
	Options   []PendingActionOption
	CreatedAt time.Time
}

type PendingActionOption struct {
	ID          string
	Label       string
	Description string
}

func NewInboxService(store inboxStore, capabilities inboxCapabilityService) *InboxService {
	return &InboxService{store: store, capabilities: capabilities}
}

func (s *InboxService) Load(ctx context.Context) (*MobileInbox, error) {
	if s == nil || s.store == nil || s.capabilities == nil {
		return nil, errors.New("inbox service is not initialized")
	}
	pendingActions, err := s.loadPendingActionSummaries(ctx)
	if err != nil {
		return nil, err
	}
	activeRuns, err := s.loadRunSummaries(ctx, s.store.ListActiveRuns, defaultInboxActiveRunLimit)
	if err != nil {
		return nil, err
	}
	recentTerminalRuns, err := s.loadRunSummaries(ctx, s.store.ListRecentTerminalRuns, defaultInboxTerminalRunLimit)
	if err != nil {
		return nil, err
	}
	snapshot := s.capabilities.Snapshot(ctx, CapabilitySnapshotOptions{ProbeMCP: false})
	return &MobileInbox{
		PendingActions:     pendingActions,
		ActiveRuns:         activeRuns,
		RecentTerminalRuns: recentTerminalRuns,
		System:             snapshot,
	}, nil
}

func (s *InboxService) loadPendingActionSummaries(ctx context.Context) ([]PendingActionSummary, error) {
	records, err := s.store.ListPendingActions(ctx, defaultInboxPendingActionLimit)
	if err != nil {
		return nil, err
	}
	items := make([]PendingActionSummary, 0, len(records))
	for _, record := range records {
		summary, err := s.projectPendingActionSummary(ctx, record)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

type runListFunc func(context.Context, int) ([]events.RunRecord, error)

func (s *InboxService) loadRunSummaries(ctx context.Context, list runListFunc, limit int) ([]RunSummary, error) {
	records, err := list(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunSummary, 0, len(records))
	for _, record := range records {
		summary, err := s.projectRunSummary(ctx, record)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

func (s *InboxService) projectPendingActionSummary(ctx context.Context, record events.PendingActionRecord) (PendingActionSummary, error) {
	run, err := s.store.LoadRun(ctx, record.RunID)
	if err != nil {
		return PendingActionSummary{}, err
	}
	return buildPendingActionSummary(record, *run)
}

func buildPendingActionSummary(record events.PendingActionRecord, run events.RunRecord) (PendingActionSummary, error) {
	if record.Status != events.PendingActionStatusPending {
		return PendingActionSummary{}, fmt.Errorf("%w: unsupported pending action status %q", ErrClientProjectionFailed, record.Status)
	}
	switch record.Kind {
	case events.PendingActionKindElicitation:
		return buildElicitationPendingActionSummary(record, run)
	case events.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionPendingActionSummary(record, run)
	default:
		return PendingActionSummary{}, fmt.Errorf("%w: unsupported pending action kind %q", ErrClientProjectionFailed, record.Kind)
	}
}

func buildElicitationPendingActionSummary(record events.PendingActionRecord, run events.RunRecord) (PendingActionSummary, error) {
	body, err := pendingActionBody(record)
	if err != nil {
		return PendingActionSummary{}, err
	}
	title := strings.TrimSpace(record.Subject)
	if title == "" {
		title = "Approval required"
	}
	return PendingActionSummary{
		ActionID: record.ActionID,
		RunID:    record.RunID,
		ThreadID: run.SessionID,
		Kind:     string(record.Kind),
		Status:   string(record.Status),
		Title:    title,
		Body:     body,
		Options: []PendingActionOption{
			{ID: "accept", Label: "Accept"},
			{ID: "decline", Label: "Decline"},
		},
		CreatedAt: record.CreatedAt,
	}, nil
}

func buildOperatorQuestionPendingActionSummary(record events.PendingActionRecord, run events.RunRecord) (PendingActionSummary, error) {
	payload, err := operatorQuestionPayload(record)
	if err != nil {
		return PendingActionSummary{}, err
	}
	title := strings.TrimSpace(record.Subject)
	if title == "" {
		title = "Operator question"
	}
	return PendingActionSummary{
		ActionID:  record.ActionID,
		RunID:     record.RunID,
		ThreadID:  run.SessionID,
		Kind:      string(record.Kind),
		Status:    string(record.Status),
		Title:     title,
		Body:      strings.TrimSpace(payload.Question),
		Options:   pendingActionOptionsFromEventOptions(payload.Options),
		CreatedAt: record.CreatedAt,
	}, nil
}

func pendingActionBody(record events.PendingActionRecord) (string, error) {
	payload, err := pendingActionPayload(record)
	if err != nil {
		return "", err
	}
	message, ok := payload["message"].(string)
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(message), nil
}

func pendingActionPayload(record events.PendingActionRecord) (map[string]any, error) {
	raw := strings.TrimSpace(record.PayloadJSON)
	if raw == "" {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("%w: pending action %s payload_json: %v", ErrClientProjectionFailed, record.ActionID, err)
	}
	return payload, nil
}

func operatorQuestionPayload(record events.PendingActionRecord) (events.OperatorQuestionPayload, error) {
	raw := strings.TrimSpace(record.PayloadJSON)
	if raw == "" {
		return events.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s payload_json is empty", ErrClientProjectionFailed, record.ActionID)
	}
	var payload events.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return events.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s payload_json: %v", ErrClientProjectionFailed, record.ActionID, err)
	}
	if strings.TrimSpace(payload.Question) == "" {
		return events.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s operator question is empty", ErrClientProjectionFailed, record.ActionID)
	}
	return payload, nil
}
