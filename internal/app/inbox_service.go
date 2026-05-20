package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

const (
	defaultInboxPendingActionLimit = 20
	defaultInboxActiveRunLimit     = 20
	defaultInboxTerminalRunLimit   = 20
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
	RunID     string
	ThreadID  string
	Status    string
	Mode      string
	CreatedAt time.Time
	UpdatedAt time.Time
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
		summary, err := projectRunSummary(record)
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

func pendingActionOptionsFromEventOptions(items []events.PendingActionOption) []PendingActionOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]PendingActionOption, 0, len(items))
	for _, item := range items {
		out = append(out, PendingActionOption{
			ID:          strings.TrimSpace(item.ID),
			Label:       strings.TrimSpace(item.Label),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func projectRunSummary(record events.RunRecord) (RunSummary, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return RunSummary{}, err
	}
	mode, err := projectRunMode(record.OrchestrationMode)
	if err != nil {
		return RunSummary{}, err
	}
	return RunSummary{
		RunID:     record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		Mode:      mode,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}
